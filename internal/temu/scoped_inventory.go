package temu

import (
	"context"
	"crypto/sha256"
	"strings"
	"time"
	"xlwms-api-manager/internal/model"
)

// Batch SKUs with identical credential scopes, without mixing credentials sharing a warehouse.
func QueryScopedInventory(ctx context.Context, scopes map[string][]model.WarehouseCredentials, timeout time.Duration, now time.Time) LiveInventoryResult {
	type group struct {
		credentials []model.WarehouseCredentials
		skus        []string
	}
	groups := map[[32]byte]*group{}
	for sku, credentials := range scopes {
		var identity strings.Builder
		for _, c := range credentials {
			identity.WriteString(c.Code + "\x00" + c.APIBaseURL + "\x00" + c.AppKey + "\x00" + c.APICredentialKey + "\x00" + c.OMSAccountKey + "\x00")
		}
		key := sha256.Sum256([]byte(identity.String()))
		if groups[key] == nil {
			groups[key] = &group{credentials: credentials}
		}
		groups[key].skus = append(groups[key].skus, sku)
	}
	outcomes := make(chan LiveInventoryResult, len(groups))
	for _, g := range groups {
		go func(g *group) {
			current := QueryLiveInventory(ctx, g.credentials, g.skus, timeout, now)
			for _, credential := range g.credentials {
				for _, sku := range g.skus {
					stock := current.InventoryBySKU[sku][credential.Code]
					stock.APIBinding = &WarehouseAPIBinding{CredentialKey: credential.APICredentialKey, OMSAccountKey: credential.OMSAccountKey}
					current.InventoryBySKU[sku][credential.Code] = stock
				}
			}
			if len(g.credentials) > 0 {
				current.Complete = true
				for i := range current.WarehouseQueries {
					q := &current.WarehouseQueries[i]
					if q.Status == QueryInactive {
						q.Status = QueryOutOfScope
						q.Error = ""
						for _, sku := range g.skus {
							current.InventoryBySKU[sku][q.WarehouseCode] = WarehouseInventory{QueryStatus: QueryOutOfScope}
						}
					} else if q.Status != QuerySucceeded {
						current.Complete = false
					}
				}
			}
			outcomes <- current
		}(g)
	}
	result := LiveInventoryResult{Complete: true, InventoryBySKU: map[string]map[string]WarehouseInventory{}}
	for range groups {
		current := <-outcomes
		result.Complete = result.Complete && current.Complete
		result.WindowStart = current.WindowStart
		result.WindowEnd = current.WindowEnd
		result.WarehouseQueries = append(result.WarehouseQueries, current.WarehouseQueries...)
		for sku, inventory := range current.InventoryBySKU {
			result.InventoryBySKU[sku] = inventory
		}
	}
	return result
}
