package main

import (
	"context"
	"errors"
	"testing"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/syncer"
)

type scheduledWarehouseSource struct {
	warehouses []model.WarehouseCredentials
	err        error
}

func (source scheduledWarehouseSource) ActiveWarehouseCredentials(context.Context) ([]model.WarehouseCredentials, error) {
	return source.warehouses, source.err
}

type scheduledSyncTrigger struct {
	calls []string
	errs  map[string]error
}

func (trigger *scheduledSyncTrigger) TriggerInventory(warehouse model.WarehouseCredentials, kinds []string, parameters map[string]map[string]any, pageSize int) ([]model.SyncRun, error) {
	trigger.calls = append(trigger.calls, warehouse.Code)
	if len(kinds) != 1 || kinds[0] != "integrated" || parameters != nil || pageSize != 100 {
		return nil, errors.New("unexpected scheduled inventory request")
	}
	if err := trigger.errs[warehouse.Code]; err != nil {
		return nil, err
	}
	return []model.SyncRun{{WarehouseCode: warehouse.Code, Target: "integrated"}}, nil
}

func TestTriggerScheduledInventorySyncRefreshesEveryActiveWarehouse(t *testing.T) {
	trigger := &scheduledSyncTrigger{errs: map[string]error{"BUSY": syncer.ErrAlreadyRunning}}
	started, skipped, err := triggerScheduledInventorySync(context.Background(), scheduledWarehouseSource{warehouses: []model.WarehouseCredentials{
		{WarehouseSummary: model.WarehouseSummary{Code: "DPSNY002"}},
		{WarehouseSummary: model.WarehouseSummary{Code: "BUSY"}},
		{WarehouseSummary: model.WarehouseSummary{Code: "ARPCA01"}},
	}}, trigger)
	if err != nil {
		t.Fatal(err)
	}
	if started != 2 || skipped != 1 || len(trigger.calls) != 3 {
		t.Fatalf("started=%d skipped=%d calls=%v", started, skipped, trigger.calls)
	}
}

func TestTriggerScheduledInventorySyncContinuesAfterWarehouseFailure(t *testing.T) {
	trigger := &scheduledSyncTrigger{errs: map[string]error{"BROKEN": errors.New("upstream failed")}}
	started, skipped, err := triggerScheduledInventorySync(context.Background(), scheduledWarehouseSource{warehouses: []model.WarehouseCredentials{
		{WarehouseSummary: model.WarehouseSummary{Code: "BROKEN"}},
		{WarehouseSummary: model.WarehouseSummary{Code: "ARPCA01"}},
	}}, trigger)
	if err == nil || started != 1 || skipped != 0 || len(trigger.calls) != 2 {
		t.Fatalf("started=%d skipped=%d calls=%v err=%v", started, skipped, trigger.calls, err)
	}
}
