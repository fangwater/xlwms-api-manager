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

const (
	scheduledFundsFlowPageSize = 100
	scheduledCostWorkers       = 4
	scheduledCostRPS           = 8
	scheduledCostMaxAttempts   = 3
	scheduledRunPollInterval   = time.Second
)

type scheduledCostStore interface {
	activeWarehouseSource
	SyncRun(context.Context, int64) (model.SyncRun, error)
}

type costSyncTrigger interface {
	TriggerFundsFlow([]model.WarehouseCredentials, map[string]any, int) ([]model.SyncRun, error)
	TriggerCostDetails(model.WarehouseCredentials, syncer.DetailOptions) (model.SyncRun, error)
}

type scheduledCostSyncStats struct {
	FundsStarted   int
	DetailsStarted int
	Skipped        int
}

type scheduledWarehouseRun struct {
	warehouse model.WarehouseCredentials
	run       model.SyncRun
}

func backgroundCostSync(ctx context.Context, source scheduledCostStore, trigger costSyncTrigger, interval, timeout time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = time.Hour
	}
	if timeout <= 0 {
		timeout = time.Hour
	}
	run := func() {
		syncCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		stats, err := triggerScheduledCostSync(syncCtx, source, trigger)
		if err != nil {
			logger.Warn("scheduled cost refresh incomplete", "funds_started", stats.FundsStarted, "details_started", stats.DetailsStarted, "skipped", stats.Skipped, "error", err)
			return
		}
		logger.Info("scheduled cost refresh completed", "funds_started", stats.FundsStarted, "details_started", stats.DetailsStarted, "skipped", stats.Skipped)
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

func triggerScheduledCostSync(ctx context.Context, source scheduledCostStore, trigger costSyncTrigger) (scheduledCostSyncStats, error) {
	var stats scheduledCostSyncStats
	warehouses, err := source.ActiveWarehouseCredentials(ctx)
	if err != nil {
		return stats, fmt.Errorf("load active warehouses: %w", err)
	}

	fundsRuns := make([]scheduledWarehouseRun, 0, len(warehouses))
	problems := make([]error, 0)
	for _, warehouse := range warehouses {
		runs, triggerErr := trigger.TriggerFundsFlow([]model.WarehouseCredentials{warehouse}, nil, scheduledFundsFlowPageSize)
		if errors.Is(triggerErr, syncer.ErrAlreadyRunning) {
			stats.Skipped++
			continue
		}
		if triggerErr != nil {
			problems = append(problems, fmt.Errorf("%s funds flow: %w", warehouse.Code, triggerErr))
			continue
		}
		stats.FundsStarted += len(runs)
		for _, run := range runs {
			fundsRuns = append(fundsRuns, scheduledWarehouseRun{warehouse: warehouse, run: run})
		}
	}

	detailRuns := make([]scheduledWarehouseRun, 0, len(fundsRuns))
	options := syncer.DetailOptions{
		Workers:           scheduledCostWorkers,
		RequestsPerSecond: scheduledCostRPS,
		MaxAttempts:       scheduledCostMaxAttempts,
	}
	for _, pending := range fundsRuns {
		if _, waitErr := waitForScheduledRun(ctx, source, pending.run.ID); waitErr != nil {
			problems = append(problems, fmt.Errorf("%s funds flow: %w", pending.warehouse.Code, waitErr))
			continue
		}
		run, triggerErr := trigger.TriggerCostDetails(pending.warehouse, options)
		if errors.Is(triggerErr, syncer.ErrAlreadyRunning) {
			stats.Skipped++
			continue
		}
		if triggerErr != nil {
			problems = append(problems, fmt.Errorf("%s cost details: %w", pending.warehouse.Code, triggerErr))
			continue
		}
		stats.DetailsStarted++
		detailRuns = append(detailRuns, scheduledWarehouseRun{warehouse: pending.warehouse, run: run})
	}

	for _, pending := range detailRuns {
		if _, waitErr := waitForScheduledRun(ctx, source, pending.run.ID); waitErr != nil {
			problems = append(problems, fmt.Errorf("%s cost details: %w", pending.warehouse.Code, waitErr))
		}
	}
	return stats, errors.Join(problems...)
}

func waitForScheduledRun(ctx context.Context, source scheduledCostStore, id int64) (model.SyncRun, error) {
	for {
		run, err := source.SyncRun(ctx, id)
		if err != nil {
			return model.SyncRun{}, err
		}
		switch run.Status {
		case "succeeded":
			return run, nil
		case "failed":
			if run.Error == "" {
				return run, errors.New("sync failed")
			}
			return run, errors.New(run.Error)
		case "running":
		default:
			return run, fmt.Errorf("unexpected sync status %q", run.Status)
		}

		timer := time.NewTimer(scheduledRunPollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return model.SyncRun{}, ctx.Err()
		case <-timer.C:
		}
	}
}
