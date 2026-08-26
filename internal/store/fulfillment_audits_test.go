package store

import (
	"reflect"
	"strings"
	"testing"

	"xlwms-api-manager/internal/model"
)

func TestFulfillmentAuditWhereCombinesTrackingDimensions(t *testing.T) {
	t.Parallel()
	clause, args := fulfillmentAuditWhere(FulfillmentAuditFilter{
		Archived:         true,
		Platform:         " TEMU ",
		ShopCode:         " Panda-Buy ",
		WarehouseCode:    " east-01 ",
		TrackingCategory: "pickup_exception",
		Query:            "LM-100",
	})
	for _, expected := range []string{
		"NOT active",
		"oms_status='outbound'",
		"platform=$1",
		"shop_code=$2",
		"wh_code=$3",
		"tracking_category=$4",
		"last_mile_tracking_number ILIKE $5",
	} {
		if !strings.Contains(clause, expected) {
			t.Fatalf("clause %q does not contain %q", clause, expected)
		}
	}
	wantArgs := []any{"temu", "panda-buy", "EAST-01", "pickup_exception", "%LM-100%"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
}

func TestFulfillmentAuditWhereListsManualResolutionsSeparately(t *testing.T) {
	t.Parallel()
	clause, args := fulfillmentAuditWhere(FulfillmentAuditFilter{ManualResolved: true, WarehouseCode: " east-01 "})
	for _, expected := range []string{"NOT active", "terminal_status<>''", "wh_code=$1"} {
		if !strings.Contains(clause, expected) {
			t.Fatalf("clause %q does not contain %q", clause, expected)
		}
	}
	if !reflect.DeepEqual(args, []any{"EAST-01"}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestValidFulfillmentAuditTerminalStatus(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"manually_fulfilled", "cancelled", "not_required", "other"} {
		if !validFulfillmentAuditTerminalStatus(status) {
			t.Errorf("valid terminal status %q was rejected", status)
		}
	}
	for _, status := range []string{"", "resolved", "manual_required"} {
		if validFulfillmentAuditTerminalStatus(status) {
			t.Errorf("invalid terminal status %q was accepted", status)
		}
	}
}

func TestCompleteFulfillmentTrackingBatchAdvancesAndWrapsWatermark(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		cursor     int64
		ids        []int64
		nextCursor int64
		wrapped    bool
	}{
		{name: "initial", ids: []int64{10, 20}, nextCursor: 20},
		{name: "forward", cursor: 20, ids: []int64{30, 40}, nextCursor: 40},
		{name: "wrap", cursor: 40, ids: []int64{50, 10}, nextCursor: 10, wrapped: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items := make([]model.FulfillmentAudit, 0, len(test.ids))
			for _, id := range test.ids {
				items = append(items, model.FulfillmentAudit{ID: id})
			}
			batch := completeFulfillmentTrackingBatch(FulfillmentTrackingBatch{PreviousCursor: test.cursor}, items)
			if batch.NextCursor != test.nextCursor || batch.Wrapped != test.wrapped {
				t.Fatalf("batch cursor=%d wrapped=%t, want cursor=%d wrapped=%t", batch.NextCursor, batch.Wrapped, test.nextCursor, test.wrapped)
			}
		})
	}
}

func TestFulfillmentTrackingCategoryClauseRejectsUnknownQueue(t *testing.T) {
	t.Parallel()
	if _, err := fulfillmentTrackingCategoryClause("unknown"); err == nil {
		t.Fatal("unknown tracking queue must be rejected")
	}
}
