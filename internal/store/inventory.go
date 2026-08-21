package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xlwms-api-manager/internal/model"

	"github.com/jackc/pgx/v5"
)

var inventoryKinds = map[string]bool{
	"integrated":      true,
	"stock_age":       true,
	"stock_flow":      true,
	"box_stock":       true,
	"box_stock_age":   true,
	"box_segment_age": true,
	"box_stock_flow":  true,
}

func (p *Postgres) SaveInventoryRecords(ctx context.Context, kind, warehouseCode string, records []map[string]any) (int, error) {
	if !inventoryKinds[kind] {
		return 0, errors.New("unknown inventory kind")
	}
	warehouseCode = strings.ToUpper(strings.TrimSpace(warehouseCode))
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin inventory sync: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`, "xlwms:inventory:"+kind+":"+warehouseCode).Scan(&locked); err != nil {
		return 0, fmt.Errorf("lock inventory sync: %w", err)
	}
	if !locked {
		return 0, errors.New("another inventory sync is already running")
	}
	snapshotToken, err := randomUUID()
	if err != nil {
		return 0, err
	}
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
			return 0, fmt.Errorf("encode inventory record: %w", err)
		}
		recordKey := inventoryRecordKey(kind, warehouseCode, raw)
		available, lockedAmount, transport := inventoryAmounts(kind, record)
		productAvailable, productLocked, productTransport := inventoryDetailAmounts(record, "productStockDtl")
		_, err = tx.Exec(ctx, `
			INSERT INTO xlwms_inventory_records (
				record_key, inventory_kind, wh_code, wh_name, sku, fnsku, product_name,
				box_type, customize_barcode, stock_type, product_type, total_amount,
				product_total_amount, box_total_amount, fba_return_total_amount,
				available_amount, lock_amount, transport_amount, product_available_amount,
				product_lock_amount, product_transport_amount, change_amount,
				stock_age, stock_age_status, statistic_date, shelf_date, operate_time,
				relate_order_type, relate_order_type_name, relate_order_no, batch_no,
				segment_one_quantity, segment_two_quantity, segment_three_quantity,
				segment_four_quantity, segment_five_quantity, raw_payload, snapshot_token
			) VALUES (
				$1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''),
				NULLIF($8, ''), NULLIF($9, ''), $10, $11, $12, $13, $14, $15,
				$16, $17, $18, $19, $20, $21, $22, $23, $24, NULLIF($25, '')::date,
				NULLIF($26, '')::date, NULLIF($27, '')::timestamp, $28, NULLIF($29, ''),
				NULLIF($30, ''), NULLIF($31, ''), $32, $33, $34, $35, $36, $37::jsonb, $38::uuid
			)
			ON CONFLICT (record_key) DO UPDATE SET
					wh_name=EXCLUDED.wh_name, sku=EXCLUDED.sku, fnsku=EXCLUDED.fnsku,
					product_name=EXCLUDED.product_name, box_type=EXCLUDED.box_type,
					customize_barcode=EXCLUDED.customize_barcode, stock_type=EXCLUDED.stock_type,
					product_type=EXCLUDED.product_type, total_amount=EXCLUDED.total_amount,
					product_total_amount=EXCLUDED.product_total_amount,
					box_total_amount=EXCLUDED.box_total_amount,
					fba_return_total_amount=EXCLUDED.fba_return_total_amount,
					available_amount=EXCLUDED.available_amount, lock_amount=EXCLUDED.lock_amount,
					transport_amount=EXCLUDED.transport_amount, change_amount=EXCLUDED.change_amount,
					product_available_amount=EXCLUDED.product_available_amount,
					product_lock_amount=EXCLUDED.product_lock_amount,
					product_transport_amount=EXCLUDED.product_transport_amount,
					stock_age=EXCLUDED.stock_age, stock_age_status=EXCLUDED.stock_age_status,
					statistic_date=EXCLUDED.statistic_date, shelf_date=EXCLUDED.shelf_date,
					operate_time=EXCLUDED.operate_time, relate_order_type=EXCLUDED.relate_order_type,
					relate_order_type_name=EXCLUDED.relate_order_type_name,
					relate_order_no=EXCLUDED.relate_order_no, batch_no=EXCLUDED.batch_no,
					segment_one_quantity=EXCLUDED.segment_one_quantity,
					segment_two_quantity=EXCLUDED.segment_two_quantity,
					segment_three_quantity=EXCLUDED.segment_three_quantity,
					segment_four_quantity=EXCLUDED.segment_four_quantity,
					segment_five_quantity=EXCLUDED.segment_five_quantity,
					raw_payload=EXCLUDED.raw_payload, snapshot_token=EXCLUDED.snapshot_token,
					last_seen_at=now()
		`, recordKey, kind, warehouseCode, stringValue(record["whName"]), stringValue(record["sku"]),
			stringValue(record["fnsku"]), stringValue(record["productName"]), stringValue(record["boxType"]),
			stringValue(record["customizeBarcode"]), intValue(record["stockType"]), intValue(record["productType"]),
			nullableValue(record["totalAmount"]), nullableValue(record["productTotalAmount"]),
			nullableValue(record["boxTotalAmount"]), nullableValue(record["fbaReturnTotalAmount"]),
			available, lockedAmount, transport, productAvailable, productLocked, productTransport,
			nullableValue(record["changeAmount"]), intValue(record["stockAge"]),
			intValue(record["stockAgeStatus"]), stringValue(record["statisticDate"]), stringValue(record["shelfDate"]),
			stringValue(record["operateTime"]), intValue(record["relateOrderType"]), stringValue(record["relateOrderTypeName"]),
			stringValue(record["relateOrderNo"]), stringValue(record["batchNo"]),
			nullableValue(record["segmentOneQuantity"]), nullableValue(record["segmentTwoQuantity"]),
			nullableValue(record["segmentThreeQuantity"]), nullableValue(record["segmentFourQuantity"]),
			nullableValue(record["segmentFiveQuantity"]), string(raw), snapshotToken)
		if err != nil {
			return 0, fmt.Errorf("upsert %s inventory: %w", kind, err)
		}
		if kind == "integrated" {
			sku := strings.TrimSpace(stringValue(record["sku"]))
			if sku != "" {
				if _, err := tx.Exec(ctx, `
					INSERT INTO xlwms_warehouse_sku_specs (warehouse_sku, product_name, source)
					VALUES ($1, NULLIF($2, ''), 'inventory')
					ON CONFLICT (warehouse_sku) DO UPDATE SET
						product_name=COALESCE(NULLIF(xlwms_warehouse_sku_specs.product_name, ''), EXCLUDED.product_name)
				`, sku, strings.TrimSpace(stringValue(record["productName"]))); err != nil {
					return 0, fmt.Errorf("discover warehouse SKU %s: %w", sku, err)
				}
			}
		}
	}
	if inventorySnapshotKind(kind) {
		if _, err := tx.Exec(ctx, `
			DELETE FROM xlwms_inventory_records
			WHERE inventory_kind=$1 AND wh_code=$2 AND snapshot_token IS DISTINCT FROM $3::uuid
		`, kind, warehouseCode, snapshotToken); err != nil {
			return 0, fmt.Errorf("remove stale %s inventory: %w", kind, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit %s inventory: %w", kind, err)
	}
	return len(records), nil
}

type InventoryFilter struct {
	Kind          string
	WarehouseCode string
	Query         string
	StockType     *int
	Page          int
	PageSize      int
}

func (p *Postgres) ListInventory(ctx context.Context, filter InventoryFilter) ([]model.InventoryRecord, int, error) {
	if filter.Kind != "" && !inventoryKinds[filter.Kind] {
		return nil, 0, errors.New("unknown inventory kind")
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 30
	}
	where := []string{"1=1"}
	args := make([]any, 0, 7)
	add := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	if filter.Kind != "" {
		where = append(where, "inventory_kind="+add(filter.Kind))
	}
	if code := strings.ToUpper(strings.TrimSpace(filter.WarehouseCode)); code != "" {
		where = append(where, "wh_code="+add(code))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		placeholder := add("%" + query + "%")
		where = append(where, "(coalesce(sku,'') ILIKE "+placeholder+" OR coalesce(product_name,'') ILIKE "+placeholder+" OR coalesce(box_type,'') ILIKE "+placeholder+" OR coalesce(customize_barcode,'') ILIKE "+placeholder+" OR coalesce(relate_order_no,'') ILIKE "+placeholder+")")
	}
	if filter.StockType != nil {
		where = append(where, "stock_type="+add(*filter.StockType))
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM xlwms_inventory_records WHERE "+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := p.pool.Query(ctx, `
		SELECT id, inventory_kind, wh_code, coalesce(wh_name,''), coalesce(sku,''), coalesce(fnsku,''),
			coalesce(product_name,''), coalesce(box_type,''), coalesce(customize_barcode,''), stock_type,
			product_type, coalesce(total_amount,0)::float8, coalesce(product_total_amount,0)::float8,
			coalesce(box_total_amount,0)::float8, coalesce(fba_return_total_amount,0)::float8,
			coalesce(available_amount,0)::float8, coalesce(lock_amount,0)::float8,
			coalesce(transport_amount,0)::float8, coalesce(product_available_amount,0)::float8,
			coalesce(product_lock_amount,0)::float8, coalesce(product_transport_amount,0)::float8,
			coalesce(change_amount,0)::float8,
			stock_age, stock_age_status, coalesce(statistic_date::text,''), coalesce(shelf_date::text,''),
			coalesce(operate_time::text,''), relate_order_type, coalesce(relate_order_type_name,''),
			coalesce(relate_order_no,''), coalesce(batch_no,''), coalesce(segment_one_quantity,0)::float8,
			coalesce(segment_two_quantity,0)::float8, coalesce(segment_three_quantity,0)::float8,
			coalesce(segment_four_quantity,0)::float8, coalesce(segment_five_quantity,0)::float8, last_seen_at
		FROM xlwms_inventory_records WHERE `+clause+`
		ORDER BY coalesce(operate_time, statistic_date::timestamp, shelf_date::timestamp) DESC NULLS LAST, id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.InventoryRecord, 0, filter.PageSize)
	for rows.Next() {
		var item model.InventoryRecord
		if err := rows.Scan(
			&item.ID, &item.Kind, &item.WarehouseCode, &item.WarehouseName, &item.SKU, &item.FNSKU,
			&item.ProductName, &item.BoxType, &item.CustomizeBarcode, &item.StockType, &item.ProductType,
			&item.TotalAmount, &item.ProductTotalAmount, &item.BoxTotalAmount, &item.FBAReturnTotalAmount,
			&item.AvailableAmount, &item.LockAmount, &item.TransportAmount,
			&item.ProductAvailableAmount, &item.ProductLockAmount, &item.ProductTransportAmount, &item.ChangeAmount,
			&item.StockAge, &item.StockAgeStatus, &item.StatisticDate, &item.ShelfDate, &item.OperateTime,
			&item.RelateOrderType, &item.RelateOrderTypeName, &item.RelateOrderNo, &item.BatchNo,
			&item.SegmentOneQuantity, &item.SegmentTwoQuantity, &item.SegmentThreeQuantity,
			&item.SegmentFourQuantity, &item.SegmentFiveQuantity, &item.LastSeenAt,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (p *Postgres) ListSKUStockLevels(ctx context.Context, filter InventoryFilter) ([]model.SKUStockLevel, int, model.SKUStockSummary, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 30
	}
	where := []string{"i.inventory_kind='integrated'", "w.is_active", "coalesce(i.sku,'')<>''"}
	args := make([]any, 0, 5)
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	warehouseCode := strings.ToUpper(strings.TrimSpace(filter.WarehouseCode))
	if warehouseCode != "" {
		where = append(where, "i.wh_code="+add(warehouseCode))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		placeholder := add("%" + query + "%")
		where = append(where, "(i.sku ILIKE "+placeholder+" OR coalesce(i.product_name,'') ILIKE "+placeholder+")")
	}
	if filter.StockType != nil {
		where = append(where, "i.stock_type="+add(*filter.StockType))
	}
	clause := strings.Join(where, " AND ")
	activeRows, err := p.pool.Query(ctx, `
		SELECT wh_code
		FROM xlwms_warehouses
		WHERE is_active AND ($1='' OR wh_code=$1)
		ORDER BY wh_code
	`, warehouseCode)
	if err != nil {
		return nil, 0, model.SKUStockSummary{}, fmt.Errorf("list active warehouses for SKU stock levels: %w", err)
	}
	activeWarehouseCodes := make([]string, 0)
	for activeRows.Next() {
		var code string
		if err := activeRows.Scan(&code); err != nil {
			activeRows.Close()
			return nil, 0, model.SKUStockSummary{}, fmt.Errorf("scan active warehouse for SKU stock levels: %w", err)
		}
		activeWarehouseCodes = append(activeWarehouseCodes, code)
	}
	activeRows.Close()
	if err := activeRows.Err(); err != nil {
		return nil, 0, model.SKUStockSummary{}, err
	}
	var summary model.SKUStockSummary
	if err := p.pool.QueryRow(ctx, `
		WITH warehouse_stock AS (
			SELECT i.sku, i.wh_code,
				bool_or(i.stock_type=0) AS has_sellable_stock,
				coalesce(sum(i.total_amount),0) AS total_amount,
				coalesce(sum(i.available_amount),0) AS available_amount,
				coalesce(sum(i.product_available_amount) FILTER (WHERE i.stock_type=0),0) AS raw_fulfillment_available_amount,
				coalesce(sum(i.lock_amount),0) AS lock_amount,
				coalesce(sum(i.transport_amount),0) AS transport_amount
			FROM xlwms_inventory_records i
			JOIN xlwms_warehouses w ON w.wh_code=i.wh_code
			WHERE `+clause+`
			GROUP BY i.sku, i.wh_code
		), effective_stock AS (
			SELECT stock.*,
				CASE WHEN c.correction_mode='subtract'
				  THEN greatest(stock.raw_fulfillment_available_amount-c.correction_amount,0)
				  WHEN c.correction_mode='absolute' THEN c.correction_amount
				  ELSE stock.raw_fulfillment_available_amount END AS fulfillment_available_amount,
				(c.wh_code IS NOT NULL) AS corrected
			FROM warehouse_stock stock
			LEFT JOIN xlwms_inventory_corrections c
			  ON c.wh_code=stock.wh_code AND c.warehouse_sku=stock.sku AND stock.has_sellable_stock
		)
		SELECT count(DISTINCT sku)::int, count(DISTINCT sku)::int,
			coalesce(sum(total_amount),0)::float8,
			coalesce(sum(available_amount),0)::float8,
			coalesce(sum(fulfillment_available_amount),0)::float8,
			coalesce(sum(raw_fulfillment_available_amount),0)::float8,
			count(*) FILTER (WHERE corrected)::int,
			coalesce(sum(lock_amount),0)::float8,
			coalesce(sum(transport_amount),0)::float8
		FROM effective_stock
	`, args...).Scan(
		&summary.SKUCount, &summary.RecordCount, &summary.TotalAmount,
		&summary.AvailableAmount, &summary.FulfillmentAvailableAmount,
		&summary.RawFulfillmentAvailableAmount, &summary.CorrectionCount,
		&summary.LockAmount, &summary.TransportAmount,
	); err != nil {
		return nil, 0, summary, fmt.Errorf("summarize SKU stock levels: %w", err)
	}
	total := summary.RecordCount
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := p.pool.Query(ctx, `
		WITH warehouse_stock AS (
			SELECT i.sku, i.wh_code, max(i.product_name) AS product_name,
				max(i.product_type) AS product_type,
				bool_or(i.stock_type=0) AS has_sellable_stock,
				coalesce(sum(i.total_amount),0) AS total_amount,
				coalesce(sum(i.available_amount),0) AS available_amount,
				coalesce(sum(i.product_available_amount) FILTER (WHERE i.stock_type=0),0) AS raw_fulfillment_available_amount,
				coalesce(sum(i.lock_amount),0) AS lock_amount,
				coalesce(sum(i.transport_amount),0) AS transport_amount,
				max(i.last_seen_at) AS last_seen_at
			FROM xlwms_inventory_records i
			JOIN xlwms_warehouses w ON w.wh_code=i.wh_code
			WHERE `+clause+`
			GROUP BY i.sku, i.wh_code
		), effective_stock AS (
			SELECT stock.*,
				CASE WHEN c.correction_mode='subtract'
				  THEN greatest(stock.raw_fulfillment_available_amount-c.correction_amount,0)
				  WHEN c.correction_mode='absolute' THEN c.correction_amount
				  ELSE stock.raw_fulfillment_available_amount END AS fulfillment_available_amount,
				(c.wh_code IS NOT NULL) AS corrected,
				coalesce(c.correction_mode,'') AS correction_mode,
				coalesce(c.correction_amount,0) AS correction_amount,
				coalesce(c.note,'') AS correction_note,
				c.updated_at AS correction_updated_at
			FROM warehouse_stock stock
			LEFT JOIN xlwms_inventory_corrections c
			  ON c.wh_code=stock.wh_code AND c.warehouse_sku=stock.sku AND stock.has_sellable_stock
		)
		SELECT sku, coalesce(max(product_name),'') AS product_name, NULL::integer AS stock_type,
			max(product_type) AS product_type,
			coalesce(sum(total_amount),0)::float8 AS total_amount,
			coalesce(sum(available_amount),0)::float8 AS available_amount,
			coalesce(sum(fulfillment_available_amount),0)::float8 AS fulfillment_available_amount,
			coalesce(sum(raw_fulfillment_available_amount),0)::float8 AS raw_fulfillment_available_amount,
			coalesce(sum(lock_amount),0)::float8 AS lock_amount,
			coalesce(sum(transport_amount),0)::float8 AS transport_amount,
			count(*)::int AS warehouse_count,
			jsonb_object_agg(wh_code, jsonb_build_object(
				'total_amount', total_amount,
				'available_amount', available_amount,
				'fulfillment_available_amount', fulfillment_available_amount,
				'raw_fulfillment_available_amount', raw_fulfillment_available_amount,
				'lock_amount', lock_amount,
				'transport_amount', transport_amount,
				'corrected', corrected,
				'correction_mode', correction_mode,
				'correction_amount', correction_amount,
				'correction_note', correction_note,
				'correction_updated_at', correction_updated_at
			)) AS warehouses,
			max(last_seen_at) AS last_seen_at
		FROM effective_stock
		GROUP BY sku
		ORDER BY available_amount ASC, sku ASC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, summary, fmt.Errorf("list SKU stock levels: %w", err)
	}
	defer rows.Close()
	items := make([]model.SKUStockLevel, 0, filter.PageSize)
	for rows.Next() {
		var item model.SKUStockLevel
		var warehouses []byte
		if err := rows.Scan(
			&item.SKU, &item.ProductName, &item.StockType, &item.ProductType,
			&item.TotalAmount, &item.AvailableAmount, &item.FulfillmentAvailableAmount,
			&item.RawFulfillmentAvailableAmount, &item.LockAmount, &item.TransportAmount,
			&item.WarehouseCount, &warehouses, &item.LastSeenAt,
		); err != nil {
			return nil, 0, summary, fmt.Errorf("scan SKU stock level: %w", err)
		}
		if err := json.Unmarshal(warehouses, &item.Warehouses); err != nil {
			return nil, 0, summary, fmt.Errorf("decode SKU warehouse stock: %w", err)
		}
		for _, code := range activeWarehouseCodes {
			if _, exists := item.Warehouses[code]; !exists {
				item.Warehouses[code] = model.SKUWarehouseStock{}
			}
		}
		item.WarehouseCount = len(activeWarehouseCodes)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, summary, err
	}
	return items, total, summary, nil
}

func (p *Postgres) InventorySummary(ctx context.Context, warehouseCode string) (model.InventorySummary, error) {
	code := strings.ToUpper(strings.TrimSpace(warehouseCode))
	result := model.InventorySummary{StockByWarehouse: make(map[string]float64), AgeBuckets: map[string]float64{"0_30": 0, "31_60": 0, "61_90": 0, "91_180": 0, "181_plus": 0}}
	if err := p.pool.QueryRow(ctx, `
		SELECT coalesce(sum(total_amount),0)::float8, coalesce(sum(available_amount),0)::float8,
			coalesce(sum(lock_amount),0)::float8, coalesce(sum(transport_amount),0)::float8,
			count(DISTINCT sku) FILTER (WHERE coalesce(sku,'')<>'' )::int
		FROM xlwms_inventory_records WHERE inventory_kind='integrated' AND ($1='' OR wh_code=$1)
	`, code).Scan(&result.TotalAmount, &result.AvailableAmount, &result.LockAmount, &result.TransportAmount, &result.SKUCount); err != nil {
		return result, err
	}
	if err := p.pool.QueryRow(ctx, `
		SELECT count(DISTINCT coalesce(box_type,'') || E'\\x00' || coalesce(customize_barcode,''))::int
		FROM xlwms_inventory_records WHERE inventory_kind='box_stock' AND ($1='' OR wh_code=$1)
	`, code).Scan(&result.BoxTypeCount); err != nil {
		return result, err
	}
	rows, err := p.pool.Query(ctx, `
		SELECT w.wh_code, coalesce(sum(i.total_amount),0)::float8
		FROM xlwms_warehouses w
		LEFT JOIN xlwms_inventory_records i
			ON i.wh_code=w.wh_code AND i.inventory_kind='integrated'
		WHERE w.is_active AND ($1='' OR w.wh_code=$1)
		GROUP BY w.wh_code ORDER BY w.wh_code
	`, code)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var wh string
		var amount float64
		if err := rows.Scan(&wh, &amount); err != nil {
			rows.Close()
			return result, err
		}
		result.StockByWarehouse[wh] = amount
	}
	rows.Close()
	rows, err = p.pool.Query(ctx, `
		WITH latest AS (
			SELECT r.wh_code, r.inventory_kind, (coalesce(r.fnsku,'')<>'') AS is_return,
				max(r.statistic_date) AS statistic_date
			FROM xlwms_inventory_records r
			JOIN xlwms_warehouses w ON w.wh_code=r.wh_code
			WHERE w.is_active
				AND r.inventory_kind IN ('stock_age','box_stock_age')
				AND r.statistic_date IS NOT NULL
				AND ($1='' OR r.wh_code=$1)
			GROUP BY r.wh_code, r.inventory_kind, (coalesce(r.fnsku,'')<>'')
		)
		SELECT CASE WHEN r.stock_age<=30 THEN '0_30' WHEN r.stock_age<=60 THEN '31_60'
			WHEN r.stock_age<=90 THEN '61_90' WHEN r.stock_age<=180 THEN '91_180' ELSE '181_plus' END,
			coalesce(sum(r.total_amount),0)::float8
		FROM xlwms_inventory_records r
		JOIN latest l ON l.wh_code=r.wh_code AND l.inventory_kind=r.inventory_kind
			AND l.is_return=(coalesce(r.fnsku,'')<>'') AND l.statistic_date=r.statistic_date
		WHERE r.stock_age IS NOT NULL
		GROUP BY 1
	`, code)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var bucket string
		var amount float64
		if err := rows.Scan(&bucket, &amount); err != nil {
			rows.Close()
			return result, err
		}
		result.AgeBuckets[bucket] = amount
	}
	rows.Close()
	result.StaleAmount = result.AgeBuckets["181_plus"]
	return result, nil
}

func inventoryRecordKey(kind, warehouseCode string, raw []byte) string {
	var fields []string
	switch kind {
	case "integrated":
		fields = []string{"sku", "stockType"}
	case "stock_age":
		fields = []string{"sku", "fnsku", "stockType", "shelfDate", "statisticDate", "stockAge"}
	case "box_stock":
		fields = []string{"boxType", "customizeBarcode"}
	case "box_stock_age":
		fields = []string{"boxType", "customizeBarcode", "stockType", "shelfDate", "statisticDate"}
	case "box_segment_age":
		fields = []string{"boxType", "customizeBarcode", "stockType", "statisticDate"}
	}
	digest := sha256.Sum256(raw)
	identityParts := []string{kind, warehouseCode}
	if len(fields) == 0 {
		identityParts = append(identityParts, hex.EncodeToString(digest[:]))
	} else {
		var record map[string]any
		if json.Unmarshal(raw, &record) != nil {
			identityParts = append(identityParts, hex.EncodeToString(digest[:]))
		} else {
			for _, field := range fields {
				identityParts = append(identityParts, stringValue(record[field]))
			}
		}
	}
	identity := sha256.Sum256([]byte(strings.Join(identityParts, ":")))
	return hex.EncodeToString(identity[:])
}

func inventorySnapshotKind(kind string) bool {
	switch kind {
	case "integrated", "stock_age", "box_stock", "box_stock_age", "box_segment_age":
		return true
	default:
		return false
	}
}

func inventoryAmounts(kind string, record map[string]any) (any, any, any) {
	if kind == "integrated" {
		available := nestedFloat(record, "productStockDtl", "availableAmount") + nestedFloat(record, "boxStockDtl", "availableAmount") + nestedFloat(record, "fbaReturnStockDtl", "availableAmount")
		locked := nestedFloat(record, "productStockDtl", "lockAmount") + nestedFloat(record, "boxStockDtl", "lockAmount") + nestedFloat(record, "fbaReturnStockDtl", "lockAmount")
		transport := nestedFloat(record, "productStockDtl", "transportAmount") + nestedFloat(record, "boxStockDtl", "transportAmount") + nestedFloat(record, "fbaReturnStockDtl", "transportAmount")
		return available, locked, transport
	}
	return nullableValue(record["availableAmount"]), nullableValue(record["lockAmount"]), nullableValue(record["transportAmount"])
}

func inventoryDetailAmounts(record map[string]any, detail string) (float64, float64, float64) {
	return nestedFloat(record, detail, "availableAmount"),
		nestedFloat(record, detail, "lockAmount"),
		nestedFloat(record, detail, "transportAmount")
}

func nestedFloat(record map[string]any, object, field string) float64 {
	value, ok := record[object].(map[string]any)
	if !ok {
		return 0
	}
	switch number := value[field].(type) {
	case float64:
		return number
	case int:
		return float64(number)
	default:
		return 0
	}
}
