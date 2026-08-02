package syncer

import (
	"testing"
	"time"
)

func TestApplyInventoryDefaultsAddsStockFlowWindow(t *testing.T) {
	parameters := map[string]any{}
	now := time.Date(2026, time.August, 2, 14, 0, 0, 0, time.Local)

	applyInventoryDefaults("stock_flow", parameters, now)

	if parameters["startTime"] != "2026-07-04" || parameters["endTime"] != "2026-08-02" {
		t.Fatalf("unexpected stock flow window: %#v", parameters)
	}
}

func TestApplyInventoryDefaultsPreservesExplicitStockFlowWindow(t *testing.T) {
	parameters := map[string]any{"startTime": "2026-01-01", "endTime": "2026-01-31"}

	applyInventoryDefaults("stock_flow", parameters, time.Date(2026, time.August, 2, 14, 0, 0, 0, time.Local))

	if parameters["startTime"] != "2026-01-01" || parameters["endTime"] != "2026-01-31" {
		t.Fatalf("explicit stock flow window was changed: %#v", parameters)
	}
}
