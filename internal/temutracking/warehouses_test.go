package temutracking

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

func TestWarehouseMappingsUsesEveryConfiguredShop(t *testing.T) {
	t.Parallel()
	requestedShops := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/temu/api/system/shops":
			_, _ = writer.Write([]byte(`{"success":true,"data":{"shops":[{"code":"shop-a","name":"Shop A"},{"code":"shop-b","name":"Shop B"}]}}`))
		case "/temu/api/warehouses":
			shop := request.Header.Get("X-Temu-Shop")
			requestedShops = append(requestedShops, shop)
			if shop == "shop-a" {
				_, _ = writer.Write([]byte(`{"success":true,"data":{"mappings":[{"oms_warehouse_key":"EAST","oms_warehouse_code":"WH-EAST","temu_warehouse_id":"PLATFORM-A","temu_warehouse_name":"East"}]}}`))
				return
			}
			_, _ = writer.Write([]byte(`{"success":true,"data":{"mappings":[{"oms_warehouse_key":"WEST","oms_warehouse_code":"WH-WEST","temu_warehouse_id":"PLATFORM-B","temu_warehouse_name":"West"}]}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	mappings, err := NewClient(server.URL+"/temu", time.Second).WarehouseMappings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(requestedShops)
	if len(mappings) != 2 || len(requestedShops) != 2 || requestedShops[0] != "shop-a" || requestedShops[1] != "shop-b" {
		t.Fatalf("mappings=%+v shops=%v", mappings, requestedShops)
	}
	byID := map[string]string{}
	for _, mapping := range mappings {
		byID[mapping.TemuWarehouseID] = mapping.OMSWarehouseCode
	}
	if byID["PLATFORM-A"] != "WH-EAST" || byID["PLATFORM-B"] != "WH-WEST" {
		t.Fatalf("unexpected mappings: %+v", byID)
	}
}

func TestWarehouseMappingsRejectsConflictingPlatformWarehouse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/system/shops" {
			_, _ = writer.Write([]byte(`{"success":true,"data":{"shops":[{"code":"a"},{"code":"b"}]}}`))
			return
		}
		code := "WH-A"
		if request.Header.Get("X-Temu-Shop") == "b" {
			code = "WH-B"
		}
		_, _ = writer.Write([]byte(`{"success":true,"data":{"mappings":[{"oms_warehouse_code":"` + code + `","temu_warehouse_id":"PLATFORM-1"}]}}`))
	}))
	defer server.Close()

	if _, err := NewClient(server.URL, time.Second).WarehouseMappings(context.Background()); err == nil {
		t.Fatal("expected conflicting mapping error")
	}
}
