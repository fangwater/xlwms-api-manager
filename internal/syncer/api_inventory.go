package syncer

import (
	"context"
	"errors"
	"time"

	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/xlwms"
)

type apiInventoryStore interface {
	APIScopeCredentials(context.Context, string) (store.APIScopeCredentials, error)
	SaveAPIScopeInventory(context.Context, string, []store.WarehouseAPIInventoryItem) error
	FailAPIScopeInventory(context.Context, string) error
}

func (s *Service) SyncAPICredentialInventory(ctx context.Context, key string) error {
	lockKey := "api-scope:" + key
	if err := s.reserve([]string{lockKey}); err != nil {
		return err
	}
	defer s.release([]string{lockKey})
	return syncAPIInventory(ctx, s.store, key, s.requestTimeout)
}

func syncAPIInventory(ctx context.Context, destination apiInventoryStore, key string, timeout time.Duration) error {
	credentials, err := destination.APIScopeCredentials(ctx, key)
	if err == nil {
		client := xlwms.NewClient(credentials.BaseURL, credentials.AppKey, credentials.AppSecret, timeout)
		var records []map[string]any
		records, err = client.DiscoverInventory(ctx)
		if err == nil {
			items := make([]store.WarehouseAPIInventoryItem, 0, len(records))
			for _, record := range records {
				items = append(items, store.WarehouseAPIInventoryItemFromRecord(record))
			}
			err = destination.SaveAPIScopeInventory(ctx, key, items)
		}
	}
	if err == nil {
		return nil
	}
	// A canceled upstream request must still leave a visible failure state.
	failureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if failErr := destination.FailAPIScopeInventory(failureCtx, key); failErr != nil {
		return errors.New("API inventory sync failed and status could not be saved")
	}
	return errors.New("API inventory scope sync failed")
}
