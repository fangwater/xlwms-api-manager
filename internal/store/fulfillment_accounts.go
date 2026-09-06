package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"xlwms-api-manager/internal/model"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidFulfillmentAccount = errors.New("invalid fulfillment account configuration")

func (p *Postgres) ListOMSAccountSummaries(ctx context.Context, includeDisabled bool) ([]model.OMSAccountSummary, error) {
	rows, err := p.pool.Query(ctx, `
SELECT account.account_key,account.account_label,account.account_hint,account.enabled,
       coalesce(array_agg(DISTINCT binding.credential_key ORDER BY binding.credential_key)
           FILTER (WHERE binding.credential_key IS NOT NULL),'{}'::text[]),
       count(DISTINCT (inventory.credential_key,inventory.warehouse_sku))
           FILTER (WHERE inventory.warehouse_sku<>''),account.updated_at,
       CASE WHEN count(binding.credential_key)=0 THEN 'unbound'
            WHEN bool_or(credential.inventory_sync_status='failed') THEN 'failed'
            WHEN bool_or(credential.inventory_sync_status<>'ready' OR NOT credential.is_active) THEN 'pending'
            ELSE 'ready' END
FROM xlwms_oms_accounts account
LEFT JOIN xlwms_oms_account_api_credentials binding ON binding.account_key=account.account_key
LEFT JOIN xlwms_api_credentials credential ON credential.credential_key=binding.credential_key
LEFT JOIN xlwms_api_credential_inventory inventory ON inventory.credential_key=binding.credential_key
WHERE account.enabled OR $1
GROUP BY account.account_key,account.account_label,account.account_hint,account.enabled,account.updated_at
ORDER BY account.account_label,account.account_key
`, includeDisabled)
	if err != nil {
		return nil, fmt.Errorf("list OMS accounts: %w", err)
	}
	defer rows.Close()
	items := make([]model.OMSAccountSummary, 0)
	for rows.Next() {
		var item model.OMSAccountSummary
		if err := rows.Scan(&item.Key, &item.Label, &item.UsernameHint, &item.Enabled,
			&item.APICredentialKeys, &item.SKUCount, &item.UpdatedAt, &item.SKUStatus); err != nil {
			return nil, fmt.Errorf("scan OMS account: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) OMSAccountSummary(ctx context.Context, accountKey string) (model.OMSAccountSummary, error) {
	accountKey = normalizeOMSAccountKey(accountKey)
	var item model.OMSAccountSummary
	err := p.pool.QueryRow(ctx, `
SELECT account.account_key,account.account_label,account.account_hint,account.enabled,
       coalesce(array_agg(DISTINCT binding.credential_key ORDER BY binding.credential_key)
           FILTER (WHERE binding.credential_key IS NOT NULL),'{}'::text[]),
       count(DISTINCT (inventory.credential_key,inventory.warehouse_sku))
           FILTER (WHERE inventory.warehouse_sku<>''),account.updated_at,
       CASE WHEN count(binding.credential_key)=0 THEN 'unbound'
            WHEN bool_or(credential.inventory_sync_status='failed') THEN 'failed'
            WHEN bool_or(credential.inventory_sync_status<>'ready' OR NOT credential.is_active) THEN 'pending'
            ELSE 'ready' END
FROM xlwms_oms_accounts account
LEFT JOIN xlwms_oms_account_api_credentials binding ON binding.account_key=account.account_key
LEFT JOIN xlwms_api_credentials credential ON credential.credential_key=binding.credential_key
LEFT JOIN xlwms_api_credential_inventory inventory ON inventory.credential_key=binding.credential_key
WHERE account.account_key=$1
GROUP BY account.account_key,account.account_label,account.account_hint,account.enabled,account.updated_at
`, accountKey).Scan(&item.Key, &item.Label, &item.UsernameHint, &item.Enabled,
		&item.APICredentialKeys, &item.SKUCount, &item.UpdatedAt, &item.SKUStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.OMSAccountSummary{}, ErrOMSAccountNotFound
	}
	if err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("get OMS account summary: %w", err)
	}
	return item, nil
}

// ReplaceOMSAccountAPICredentials assigns each OpenAPI SKU scope to this OMS
// shipping account. Reassigning a scope automatically removes its old account.
func (p *Postgres) ReplaceOMSAccountAPICredentials(ctx context.Context, accountKey string, credentialKeys []string) (model.OMSAccountSummary, error) {
	accountKey = normalizeOMSAccountKey(accountKey)
	credentialKeys, err := normalizeAPICredentialKeys(credentialKeys)
	if err != nil {
		return model.OMSAccountSummary{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("begin OMS account API binding update: %w", err)
	}
	defer tx.Rollback(ctx)
	var accountExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM xlwms_oms_accounts WHERE account_key=$1)`, accountKey).Scan(&accountExists); err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("check OMS account: %w", err)
	}
	if !accountExists {
		return model.OMSAccountSummary{}, ErrOMSAccountNotFound
	}
	if len(credentialKeys) > 0 {
		var credentialCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM xlwms_api_credentials WHERE credential_key=ANY($1)`, credentialKeys).Scan(&credentialCount); err != nil {
			return model.OMSAccountSummary{}, fmt.Errorf("check OMS account API credentials: %w", err)
		}
		if credentialCount != len(credentialKeys) {
			return model.OMSAccountSummary{}, fmt.Errorf("%w: unknown OpenAPI credential", ErrInvalidFulfillmentAccount)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM xlwms_oms_account_api_credentials WHERE account_key=$1`, accountKey); err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("clear OMS account API credentials: %w", err)
	}
	for _, credentialKey := range credentialKeys {
		if _, err := tx.Exec(ctx, `
INSERT INTO xlwms_oms_account_api_credentials(credential_key,account_key,updated_at)
VALUES($1,$2,now())
ON CONFLICT(credential_key) DO UPDATE SET account_key=EXCLUDED.account_key,updated_at=now()
`, credentialKey, accountKey); err != nil {
			return model.OMSAccountSummary{}, fmt.Errorf("save OMS account API credential: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.OMSAccountSummary{}, fmt.Errorf("commit OMS account API binding update: %w", err)
	}
	return p.OMSAccountSummary(ctx, accountKey)
}

func normalizeAPICredentialKeys(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" || len(key) > 100 {
			return nil, fmt.Errorf("%w: invalid OpenAPI credential key", ErrInvalidFulfillmentAccount)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	return result, nil
}
