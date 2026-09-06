package syncer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xlwms-api-manager/internal/store"
)

type scopeTestStore struct {
	url    string
	saved  bool
	failed bool
	items  []store.WarehouseAPIInventoryItem
	key    string
}

func (s *scopeTestStore) APIScopeCredentials(_ context.Context, key string) (store.APIScopeCredentials, error) {
	s.key = key
	return store.APIScopeCredentials{BaseURL: s.url, AppKey: "test-key", AppSecret: "test-secret"}, nil
}
func (s *scopeTestStore) SaveAPIScopeInventory(_ context.Context, key string, items []store.WarehouseAPIInventoryItem) error {
	s.saved = true
	s.items = items
	return nil
}
func (s *scopeTestStore) FailAPIScopeInventory(_ context.Context, key string) error {
	s.failed = true
	return nil
}

func TestSyncAPIScopeSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name      string
		responses []string
		want      int
		failed    bool
	}{
		{"multiple pages", []string{`{"code":200,"data":{"pages":2,"records":[{"whCode":"WH1","sku":"SKU1"}]}}`, `{"code":200,"data":{"pages":2,"records":[{"whCode":"WH2","sku":"SKU2"}]}}`}, 2, false},
		{"empty success", []string{`{"code":200,"data":{"pages":0,"records":[]}}`}, 0, false},
		{"invalid records", []string{`{"code":200,"data":{}}`}, 1, true},
		{"later page failure", []string{`{"code":200,"data":{"pages":2,"records":[{"whCode":"WH1","sku":"SKU1"}]}}`, `{"code":500,"msg":"failed"}`}, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls >= len(tc.responses) {
					t.Error("unexpected page request")
					http.Error(w, "failed", 500)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.responses[calls])
				calls++
			}))
			defer server.Close()
			dest := &scopeTestStore{url: server.URL, items: []store.WarehouseAPIInventoryItem{{WarehouseCode: "OLD", WarehouseSKU: "OLD"}}}
			err := syncAPIInventory(context.Background(), dest, "scope-one", time.Second)
			if (err != nil) != tc.failed || dest.failed != tc.failed || dest.saved == tc.failed {
				t.Fatalf("unexpected sync status: err=%v saved=%v failed=%v", err, dest.saved, dest.failed)
			}
			if len(dest.items) != tc.want || dest.key != "scope-one" {
				t.Fatal("incorrect scope snapshot")
			}
			if tc.failed && dest.items[0].WarehouseSKU != "OLD" {
				t.Fatal("failure replaced previous snapshot")
			}
		})
	}
}
