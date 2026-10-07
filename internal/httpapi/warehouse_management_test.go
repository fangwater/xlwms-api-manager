package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"xlwms-api-manager/internal/credentials"
	"xlwms-api-manager/internal/fulfillment"
	"xlwms-api-manager/internal/oms"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/temutracking"
)

type warehouseOptionsFake struct{}

func (warehouseOptionsFake) WarehouseMappings(context.Context) ([]temutracking.WarehouseMapping, error) {
	return nil, nil
}
func (warehouseOptionsFake) PlatformWarehouseOptions(_ context.Context, platform, shop, _ string) ([]fulfillment.PlatformWarehouseOption, error) {
	if platform != "shein" || shop != "beauty-hangers-home" {
		return []fulfillment.PlatformWarehouseOption{}, nil
	}
	return []fulfillment.PlatformWarehouseOption{{ID: "WH-VERIFIED-HOUSTON", Name: "Houston", OMSCode: "ARP06A", CanShip: true}}, nil
}

func TestWarehouseManagementHTTPChecksActivationAndPause(t *testing.T) {
	databaseURL := os.Getenv("XLWMS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("XLWMS_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("integration connection failed")
	}
	schema := fmt.Sprintf("warehouse_http_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		_ = conn.Close(context.Background())
	}()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("integration URL invalid")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	cipher, err := credentials.EnsureKeyFile(filepath.Join(t.TempDir(), "test.key"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.NewPostgres(ctx, parsed.String(), cipher)
	if err != nil {
		t.Fatal("test store creation failed")
	}
	defer p.Close()
	if err = p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	inventory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":200,"data":{"records":[],"total":0,"pages":0}}`)
	}))
	defer inventory.Close()
	group, err := p.UpsertWarehouseAPICredentialGroup(ctx, "test scope", inventory.URL, "test-app-key", "test-app-secret", []store.WarehouseAPIInventoryItem{{WarehouseCode: "ARP06A", WarehouseSKU: "TEST-SKU"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.CreateOMSAccount(ctx, "test-hangers", "Test account", "test-user", "test-password", []string{group.Key}); err != nil {
		t.Fatal(err)
	}
	operator := &fakePlatformOrders{warehouses: nil}
	server := &Server{store: p, platformMappings: warehouseOptionsFake{}, platformAccounts: fixedPlatformOrderAccounts{operator: operator}, consoleUser: "test-console", consolePassword: "test-password", requestTimeout: 5 * time.Second, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	server.registerWarehouseManagementRoutes(mux)
	call := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			r.SetBasicAuth("test-console", "test-password")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	draft := `{"platform_warehouse_id":"WH-VERIFIED-HOUSTON","platform_warehouse_name":"Houston","revision":0,"warehouse_revision":1}`
	path := "/v1/fulfillment-warehouses/ARP_HOUSTON/platform-bindings/shein/beauty-hangers-home"
	if w := call("PUT", path, draft, false); w.Code != 401 {
		t.Fatalf("unauthenticated write accepted: %d", w.Code)
	}
	if w := call("PUT", path, draft, true); w.Code != 200 {
		t.Fatalf("draft failed: %s", w.Body.String())
	}
	input := `{"platform":"shein","shop_code":"beauty-hangers-home","revision":1,"warehouse_revision":1}`
	checkPath := "/v1/fulfillment-warehouses/ARP_HOUSTON/readiness"
	activationPath := "/v1/fulfillment-warehouses/ARP_HOUSTON/activation"
	w := call("POST", checkPath, input, true)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result struct {
		Data fulfillment.WarehouseReadiness `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Ready {
		t.Fatal("missing OMS warehouse permission passed")
	}
	if w := call("POST", activationPath, input, true); w.Code != 409 {
		t.Fatal("activation accepted without permission")
	}
	operator.warehouses = []oms.WarehouseOption{{WarehouseCode: "ARP06A"}}
	w = call("POST", checkPath, input, true)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if !result.Data.Ready {
		t.Fatalf("zero stock incorrectly blocked configuration: %s", w.Body.String())
	}
	if w := call("POST", activationPath, input, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	bindings, err := p.WarehouseBindings(ctx, "ARP_HOUSTON", "shein", "beauty-hangers-home")
	if err != nil || len(bindings) != 1 || !bindings[0].Effective {
		t.Fatal("activation failed to become effective")
	}
	other, err := p.WarehouseBindings(ctx, "ARP_HOUSTON", "temu", "panda-homes")
	if err != nil || other[0].Effective {
		t.Fatal("activation leaked to another platform")
	}
	if w := call("POST", activationPath, input, true); w.Code != 409 {
		t.Fatal("stale activation accepted")
	}
	if w := call("PATCH", "/v1/fulfillment-warehouses/ARP_HOUSTON", `{"enabled":false,"warehouse_revision":2}`, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	bindings, err = p.WarehouseBindings(ctx, "ARP_HOUSTON", "shein", "beauty-hangers-home")
	if err != nil || bindings[0].Effective || !bindings[0].Enabled {
		t.Fatal("global pause did not stop new fulfillment or destroyed shop configuration")
	}
}
