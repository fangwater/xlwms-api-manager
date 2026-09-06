package main

import (
	"context"
	"log/slog"
	"time"

	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/syncer"
)

func backgroundAPIScopeSync(ctx context.Context, source *store.Postgres, service *syncer.Service, interval, timeout time.Duration, logger *slog.Logger) {
	run := func() {
		listCtx, cancel := context.WithTimeout(ctx, timeout)
		groups, err := source.ListWarehouseAPICredentialGroups(listCtx, false)
		cancel()
		if err != nil {
			logger.Warn("list API scopes for refresh failed")
			return
		}
		for _, group := range groups {
			if ctx.Err() != nil {
				return
			}
			runCtx, cancel := context.WithTimeout(ctx, timeout)
			err := service.SyncAPICredentialInventory(runCtx, group.Key)
			cancel()
			if err != nil {
				logger.Warn("API inventory scope refresh failed", "credential", group.Key)
			}
		}
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
