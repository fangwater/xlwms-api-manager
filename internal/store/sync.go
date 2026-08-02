package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (p *Postgres) ReplaceFundsFlowSnapshot(ctx context.Context, warehouseCode string, records []map[string]any) (int, error) {
	warehouseCode = strings.ToUpper(strings.TrimSpace(warehouseCode))
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin funds flow snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`, "xlwms:funds-flow:"+warehouseCode).Scan(&locked); err != nil {
		return 0, fmt.Errorf("lock funds flow snapshot: %w", err)
	}
	if !locked {
		return 0, errors.New("another funds flow sync is already running")
	}
	snapshotToken, err := randomUUID()
	if err != nil {
		return 0, err
	}
	occurrences := make(map[string]int)
	for _, record := range records {
		returnedCode := strings.ToUpper(strings.TrimSpace(stringValue(record["whCode"])))
		if returnedCode == "" {
			returnedCode = warehouseCode
		}
		if returnedCode != warehouseCode {
			return 0, fmt.Errorf("warehouse response mismatch: expected %s, got %s", warehouseCode, returnedCode)
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return 0, fmt.Errorf("encode funds flow: %w", err)
		}
		payloadHash := payloadHash(raw)
		occurrences[payloadHash]++
		sourceKey, err := FundsFlowSourceKey(warehouseCode, record, occurrences[payloadHash])
		if err != nil {
			return 0, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO xlwms_funds_flows (
				source_key, wh_code, order_no, platform_order_no, cost_total,
				currency_code, cost_status, module_type, cost_time, bill_status,
				relate_bill_no, raw_payload, snapshot_token
			) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, NULLIF($6, ''),
				$7, $8, NULLIF($9, '')::timestamp, $10, NULLIF($11, ''), $12::jsonb, $13::uuid)
			ON CONFLICT (source_key) DO UPDATE SET
				cost_total=EXCLUDED.cost_total, cost_status=EXCLUDED.cost_status,
				bill_status=EXCLUDED.bill_status, relate_bill_no=EXCLUDED.relate_bill_no,
				raw_payload=EXCLUDED.raw_payload, snapshot_token=EXCLUDED.snapshot_token,
				last_seen_at=now()
		`, sourceKey, warehouseCode, stringValue(record["orderNo"]), stringValue(record["platformOrderNo"]),
			nullableValue(record["costTotal"]), stringValue(record["currencyCode"]), intValue(record["costStatus"]),
			intValue(record["moduleType"]), stringValue(record["costTime"]), intValue(record["billStatus"]),
			stringValue(record["relateBillNo"]), string(raw), snapshotToken)
		if err != nil {
			return 0, fmt.Errorf("upsert funds flow: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM xlwms_funds_flows WHERE wh_code=$1 AND snapshot_token IS DISTINCT FROM $2::uuid`, warehouseCode, snapshotToken); err != nil {
		return 0, fmt.Errorf("remove stale funds flows: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit funds flow snapshot: %w", err)
	}
	return len(records), nil
}

type DetailTarget struct {
	OrderNo    string
	ModuleType *int
}

func (p *Postgres) PendingDetailTargets(ctx context.Context, warehouseCode string, limit int) ([]DetailTarget, error) {
	query := `
		SELECT order_no, module_type
		FROM xlwms_funds_flows
		WHERE wh_code=$1 AND coalesce(order_no, '') <> ''
		GROUP BY order_no, module_type
		HAVING bool_and(detail_sync_status='success') = false
		ORDER BY max(cost_time) DESC NULLS LAST, order_no
	`
	args := []any{strings.ToUpper(strings.TrimSpace(warehouseCode))}
	if limit > 0 {
		query += " LIMIT $2"
		args = append(args, limit)
	}
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load pending cost details: %w", err)
	}
	defer rows.Close()
	targets := make([]DetailTarget, 0)
	for rows.Next() {
		var target DetailTarget
		if err := rows.Scan(&target.OrderNo, &target.ModuleType); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (p *Postgres) SaveCostDetail(ctx context.Context, warehouseCode, queryOrderNo string, queryOrderType int, targetModuleType *int, detail map[string]any, attempts int) (int, error) {
	warehouseCode = strings.ToUpper(strings.TrimSpace(warehouseCode))
	costNo := strings.TrimSpace(stringValue(detail["costNo"]))
	if costNo == "" {
		return 0, errors.New("costDetail response is missing costNo")
	}
	items, ok := detail["costItemList"].([]any)
	if detail["costItemList"] != nil && !ok {
		return 0, errors.New("costDetail costItemList must be an array")
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return 0, err
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	detailModuleType := intValue(detail["moduleType"])
	if detailModuleType == nil {
		detailModuleType = targetModuleType
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO xlwms_cost_details (
			wh_code, cost_no, query_order_no, query_order_type, cost_total,
			bill_status, module_type, currency_code, cost_status, create_time,
			platform_order_no, relate_bill_no, raw_payload
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9,
			NULLIF($10, '')::timestamp, NULLIF($11, ''), NULLIF($12, ''), $13::jsonb)
		ON CONFLICT (wh_code, cost_no) DO UPDATE SET
			query_order_no=EXCLUDED.query_order_no, query_order_type=EXCLUDED.query_order_type,
			cost_total=EXCLUDED.cost_total, bill_status=EXCLUDED.bill_status,
			module_type=EXCLUDED.module_type, currency_code=EXCLUDED.currency_code,
			cost_status=EXCLUDED.cost_status, create_time=EXCLUDED.create_time,
			platform_order_no=EXCLUDED.platform_order_no, relate_bill_no=EXCLUDED.relate_bill_no,
			raw_payload=EXCLUDED.raw_payload, last_fetched_at=now()
	`, warehouseCode, costNo, queryOrderNo, queryOrderType, nullableValue(detail["costTotal"]),
		intValue(detail["billStatus"]), detailModuleType, stringValue(detail["currencyCode"]),
		intValue(detail["costStatus"]), stringValue(detail["createTime"]), stringValue(detail["platformOrderNo"]),
		stringValue(detail["relateBillNo"]), string(raw))
	if err != nil {
		return 0, fmt.Errorf("upsert cost detail: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM xlwms_cost_items WHERE wh_code=$1 AND cost_no=$2`, warehouseCode, costNo); err != nil {
		return 0, err
	}
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return 0, errors.New("costDetail contains an invalid cost item")
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO xlwms_cost_items (
				wh_code, cost_no, item_index, bill_item_name, bill_item_total, charge_time, raw_payload
			) VALUES ($1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, '')::timestamp, $7::jsonb)
		`, warehouseCode, costNo, index, stringValue(item["billItemName"]), nullableValue(item["billItemTotal"]), stringValue(item["chargeTime"]), string(encoded)); err != nil {
			return 0, fmt.Errorf("insert cost item: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE xlwms_funds_flows SET detail_sync_status='success',
			detail_attempts=detail_attempts+$1, detail_last_attempt_at=now(),
			detail_error_code=NULL, detail_error_message=NULL
		WHERE wh_code=$2 AND order_no=$3 AND module_type IS NOT DISTINCT FROM $4
	`, attempts, warehouseCode, queryOrderNo, targetModuleType); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(items), nil
}

func (p *Postgres) MarkCostDetailError(ctx context.Context, warehouseCode string, target DetailTarget, attempts int, code, message string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE xlwms_funds_flows SET detail_sync_status='error',
			detail_attempts=detail_attempts+$1, detail_last_attempt_at=now(),
			detail_error_code=NULLIF($2, ''), detail_error_message=NULLIF(left($3, 1000), '')
		WHERE wh_code=$4 AND order_no=$5 AND module_type IS NOT DISTINCT FROM $6
	`, attempts, code, message, strings.ToUpper(strings.TrimSpace(warehouseCode)), target.OrderNo, target.ModuleType)
	return err
}

func payloadHash(raw []byte) string {
	hash := sha256Sum(raw)
	return hex.EncodeToString(hash)
}

func sha256Sum(raw []byte) []byte {
	hash := sha256Bytes(raw)
	return hash[:]
}

func sha256Bytes(raw []byte) [32]byte {
	return sha256.Sum256(raw)
}

func randomUUID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func intValue(value any) any {
	if value == nil || value == "" {
		return nil
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, err := strconv.Atoi(typed)
		if err == nil {
			return parsed
		}
	}
	return value
}

func nullableValue(value any) any {
	if value == nil || value == "" {
		return nil
	}
	return value
}
