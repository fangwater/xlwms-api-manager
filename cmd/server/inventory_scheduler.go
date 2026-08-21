package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/syncer"
)

const scheduledInventoryPageSize = 100

var scheduledInventoryKinds = []string{"integrated"}

type activeWarehouseSource interface {
	ActiveWarehouseCredentials(context.Context) ([]model.WarehouseCredentials, error)
}

type inventorySyncTrigger interface {
	TriggerInventory(model.WarehouseCredentials, []string, map[string]map[string]any, int) ([]model.SyncRun, error)
}

func backgroundInventorySync(ctx context.Context, warehouses activeWarehouseSource, trigger inventorySyncTrigger, interval time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = 3 * time.Minute
	}
	run := func() {
		started, skipped, err := triggerScheduledInventorySync(ctx, warehouses, trigger)
		if err != nil {
			logger.Warn("scheduled inventory refresh incomplete", "started", started, "skipped", skipped, "error", err)
			return
		}
		logger.Info("scheduled inventory refresh submitted", "started", started, "skipped", skipped)
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func triggerScheduledInventorySync(ctx context.Context, warehouses activeWarehouseSource, trigger inventorySyncTrigger) (int, int, error) {
	active, err := warehouses.ActiveWarehouseCredentials(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("load active warehouses: %w", err)
	}
	started := 0
	skipped := 0
	problems := make([]error, 0)
	for _, warehouse := range active {
		runs, triggerErr := trigger.TriggerInventory(warehouse, scheduledInventoryKinds, nil, scheduledInventoryPageSize)
		if errors.Is(triggerErr, syncer.ErrAlreadyRunning) {
			skipped++
			continue
		}
		if triggerErr != nil {
			problems = append(problems, fmt.Errorf("%s: %w", warehouse.Code, triggerErr))
			continue
		}
		started += len(runs)
	}
	return started, skipped, errors.Join(problems...)
}
