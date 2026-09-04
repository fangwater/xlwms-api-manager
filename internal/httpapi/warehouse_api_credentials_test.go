package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xlwms-api-manager/internal/xlwms"
)

func TestDiscoverWarehouseAPIInventoryWithoutWarehouseFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/integratedInventory/pageOpen" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":200,"msg":"ok","data":{"pages":1,"total":2,"records":[{"whCode":"ARPCA01","sku":"SKU-1"},{"whCode":"HYTX30","sku":"SKU-2"}]}}`))
	}))
	defer server.Close()
	client := xlwms.NewClient(server.URL, "key", "secret", time.Second)
	records, err := discoverWarehouseAPIInventory(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %#v", records)
	}
}
