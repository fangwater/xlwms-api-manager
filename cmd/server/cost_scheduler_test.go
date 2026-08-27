package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/syncer"
)

type scheduledCostSourceFake struct {
	warehouses []model.WarehouseCredentials
	runs       map[int64]model.SyncRun
	err        error
}

func (source *scheduledCostSourceFake) ActiveWarehouseCredentials(context.Context) ([]model.WarehouseCredentials, error) {
	return source.warehouses, source.err
}

func (source *scheduledCostSourceFake) SyncRun(_ context.Context, id int64) (model.SyncRun, error) {
	run, ok := source.runs[id]
	if !ok {
		return model.SyncRun{}, errors.New("run not found")
	}
	return run, nil
}

type scheduledCostTriggerFake struct {
	source       *scheduledCostSourceFake
	events       []string
	fundsErrors  map[string]error
	detailErrors map[string]error
	fundsStatus  map[string]model.SyncRun
	nextID       int64
}

func (trigger *scheduledCostTriggerFake) TriggerFundsFlow(warehouses []model.WarehouseCredentials, parameters map[string]any, pageSize int) ([]model.SyncRun, error) {
	if len(warehouses) != 1 || parameters != nil || pageSize != scheduledFundsFlowPageSize {
		return nil, errors.New("unexpected scheduled funds-flow request")
	}
	warehouse := warehouses[0]
	trigger.events = append(trigger.events, "funds:"+warehouse.Code)
	if err := trigger.fundsErrors[warehouse.Code]; err != nil {
		return nil, err
	}
	trigger.nextID++
	run := trigger.fundsStatus[warehouse.Code]
	run.ID = trigger.nextID
	run.WarehouseCode = warehouse.Code
	run.Target = "funds_flow"
	if run.Status == "" {
		run.Status = "succeeded"
	}
	trigger.source.runs[run.ID] = run
	return []model.SyncRun{run}, nil
}

func (trigger *scheduledCostTriggerFake) TriggerCostDetails(warehouse model.WarehouseCredentials, options syncer.DetailOptions) (model.SyncRun, error) {
	trigger.events = append(trigger.events, "details:"+warehouse.Code)
	if options.Workers != scheduledCostWorkers || options.RequestsPerSecond != scheduledCostRPS || options.MaxAttempts != scheduledCostMaxAttempts || options.Limit != 0 {
		return model.SyncRun{}, errors.New("unexpected scheduled cost-detail request")
	}
	if err := trigger.detailErrors[warehouse.Code]; err != nil {
		return model.SyncRun{}, err
	}
	trigger.nextID++
	run := model.SyncRun{ID: trigger.nextID, WarehouseCode: warehouse.Code, Target: "cost_details", Status: "succeeded"}
	trigger.source.runs[run.ID] = run
	return run, nil
}

func newScheduledCostFakes(codes ...string) (*scheduledCostSourceFake, *scheduledCostTriggerFake) {
	warehouses := make([]model.WarehouseCredentials, 0, len(codes))
	for _, code := range codes {
		warehouses = append(warehouses, model.WarehouseCredentials{WarehouseSummary: model.WarehouseSummary{Code: code}})
	}
	source := &scheduledCostSourceFake{warehouses: warehouses, runs: make(map[int64]model.SyncRun)}
	trigger := &scheduledCostTriggerFake{
		source:       source,
		fundsErrors:  make(map[string]error),
		detailErrors: make(map[string]error),
		fundsStatus:  make(map[string]model.SyncRun),
	}
	return source, trigger
}

func TestTriggerScheduledCostSyncRefreshesEveryActiveWarehouseInOrder(t *testing.T) {
	source, trigger := newScheduledCostFakes("DPSNY002", "ARPCA01")
	stats, err := triggerScheduledCostSync(context.Background(), source, trigger)
	if err != nil {
		t.Fatal(err)
	}
	if stats != (scheduledCostSyncStats{FundsStarted: 2, DetailsStarted: 2}) {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	want := []string{"funds:DPSNY002", "funds:ARPCA01", "details:DPSNY002", "details:ARPCA01"}
	if !reflect.DeepEqual(trigger.events, want) {
		t.Fatalf("events=%v want=%v", trigger.events, want)
	}
}

func TestTriggerScheduledCostSyncSkipsBusyWarehouseAndContinues(t *testing.T) {
	source, trigger := newScheduledCostFakes("BUSY", "ARPCA01")
	trigger.fundsErrors["BUSY"] = syncer.ErrAlreadyRunning
	stats, err := triggerScheduledCostSync(context.Background(), source, trigger)
	if err != nil {
		t.Fatal(err)
	}
	if stats != (scheduledCostSyncStats{FundsStarted: 1, DetailsStarted: 1, Skipped: 1}) {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	want := []string{"funds:BUSY", "funds:ARPCA01", "details:ARPCA01"}
	if !reflect.DeepEqual(trigger.events, want) {
		t.Fatalf("events=%v want=%v", trigger.events, want)
	}
}

func TestTriggerScheduledCostSyncDoesNotFetchDetailsAfterFailedFundsFlow(t *testing.T) {
	source, trigger := newScheduledCostFakes("BROKEN", "ARPCA01")
	trigger.fundsStatus["BROKEN"] = model.SyncRun{Status: "failed", Error: "upstream failed"}
	stats, err := triggerScheduledCostSync(context.Background(), source, trigger)
	if err == nil || stats.FundsStarted != 2 || stats.DetailsStarted != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	want := []string{"funds:BROKEN", "funds:ARPCA01", "details:ARPCA01"}
	if !reflect.DeepEqual(trigger.events, want) {
		t.Fatalf("events=%v want=%v", trigger.events, want)
	}
}
