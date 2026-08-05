package auditor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/xlwms"
)

type Service struct {
	store          *store.Postgres
	requestTimeout time.Duration
	logger         *slog.Logger
}

type CheckStats struct {
	Checked int `json:"checked"`
	Matched int `json:"matched"`
	Missing int `json:"missing"`
	Pending int `json:"pending"`
	Failed  int `json:"failed"`
	Synced  int `json:"synced"`
}

const (
	outboundPageSize = 50
	outboundMaxPages = 500
	outboundWindow   = time.Hour
	// A new installation backfills once, then advances only from its persisted watermark.
	outboundBootstrapLookback = 24 * time.Hour
)

func New(destination *store.Postgres, requestTimeout time.Duration, logger *slog.Logger) *Service {
	return &Service{store: destination, requestTimeout: requestTimeout, logger: logger}
}

func (s *Service) Check(ctx context.Context, limit int) (CheckStats, error) {
	stats := CheckStats{}
	synced, syncErr := s.syncOutboundOrders(ctx, time.Now())
	stats.Synced = synced

	candidates, err := s.store.FulfillmentAuditCandidates(ctx, limit)
	if err != nil {
		return stats, errors.Join(syncErr, err)
	}
	stats.Checked = len(candidates)
	if len(candidates) == 0 {
		return stats, syncErr
	}
	references := make([]string, 0, len(candidates))
	for _, item := range candidates {
		references = append(references, item.PlatformOrderNo)
	}
	records, err := s.store.OutboundOrdersByReferences(ctx, references)
	if err != nil {
		return stats, errors.Join(syncErr, err)
	}
	coverageStart, coverageThrough, err := s.store.OutboundSyncCoverage(ctx)
	if err != nil {
		return stats, errors.Join(syncErr, err)
	}
	matches := matchIndexedOutboundRecords(candidates, records)
	for _, item := range candidates {
		var resolution model.FulfillmentAuditResolution
		if record, exists := matches[item.ID]; exists {
			resolution = resolutionFromRecord(record)
		} else if syncErr != nil {
			resolution = model.FulfillmentAuditResolution{OMSStatus: "query_error", SyncError: syncErr.Error()}
		} else if withinOutboundCoverage(item.PlatformShippingAt, coverageStart, coverageThrough) {
			resolution = model.FulfillmentAuditResolution{OMSStatus: "not_found"}
		} else if item.OutboundOrderNo != "" && item.OMSStatus != "not_found" && item.OMSStatus != "pending_query" && item.OMSStatus != "query_error" {
			resolution = resolutionFromAudit(item)
		} else {
			resolution = model.FulfillmentAuditResolution{OMSStatus: "pending_query"}
		}
		if err := s.store.UpdateFulfillmentAuditResolution(ctx, item.ID, resolution); err != nil {
			return stats, errors.Join(syncErr, err)
		}
		switch resolution.OMSStatus {
		case "query_error":
			stats.Failed++
		case "not_found":
			stats.Missing++
		case "pending_query":
			stats.Pending++
		default:
			stats.Matched++
		}
	}
	if err := s.store.RefreshFulfillmentAuditCategories(ctx); err != nil {
		return stats, errors.Join(syncErr, err)
	}
	return stats, syncErr
}

func (s *Service) syncOutboundOrders(ctx context.Context, now time.Time) (int, error) {
	credentials, err := s.store.ActiveWarehouseCredentials(ctx)
	if err != nil {
		return 0, err
	}
	credentials = uniqueWarehouseCredentials(credentials)
	if len(credentials) == 0 {
		return 0, errors.New("no active XLWMS warehouse credentials")
	}
	through := outboundSyncThrough(now)
	total := 0
	var syncErrors []error
	for _, credential := range credentials {
		accountKey := outboundAccountKey(credential)
		watermark, err := s.store.OutboundSyncWatermark(ctx, accountKey)
		if err != nil {
			syncErrors = append(syncErrors, err)
			continue
		}
		cursor := outboundSyncCursor(through, watermark)
		for cursor.Before(through) {
			windowEnd := cursor.Add(outboundWindow)
			if windowEnd.After(through) {
				windowEnd = through
			}
			records, fetchErr := s.fetchOutboundWindow(ctx, credential, cursor, windowEnd)
			if fetchErr != nil {
				_ = s.store.MarkOutboundSyncFailure(context.WithoutCancel(ctx), accountKey, credential.Code, fetchErr)
				syncErrors = append(syncErrors, fmt.Errorf("sync outbound account %s: %w", credential.Code, fetchErr))
				break
			}
			if err := s.store.SaveOutboundSyncWindow(ctx, accountKey, credential.Code, cursor, windowEnd, records); err != nil {
				syncErrors = append(syncErrors, err)
				break
			}
			s.logger.Info("outbound order watermark advanced", "warehouse", credential.Code,
				"through", windowEnd.Format("2006-01-02 15:04:05"), "records", len(records))
			total += len(records)
			cursor = windowEnd
		}
	}
	return total, errors.Join(syncErrors...)
}

func (s *Service) fetchOutboundWindow(ctx context.Context, credential model.WarehouseCredentials, start, end time.Time) ([]model.OutboundOrderIndex, error) {
	client := xlwms.NewClient(credential.APIBaseURL, credential.AppKey, credential.AppSecret, s.requestTimeout)
	records := make([]model.OutboundOrderIndex, 0)
	for page := 1; page <= outboundMaxPages; page++ {
		result, err := client.Outbound(ctx, "parcel-list", map[string]any{
			"page": page, "pageSize": outboundPageSize, "timeType": "orderCreateTime",
			"startTime": start.Format("2006-01-02 15:04:05"), "endTime": end.Format("2006-01-02 15:04:05"),
		})
		if err != nil {
			return nil, err
		}
		pageRecords, err := xlwms.OutboundOrderRecords(result)
		if err != nil {
			return nil, err
		}
		for _, record := range pageRecords {
			records = append(records, indexedOutboundOrder(record))
		}
		if len(pageRecords) < outboundPageSize {
			return records, nil
		}
	}
	return nil, fmt.Errorf("outbound window exceeds %d records", outboundPageSize*outboundMaxPages)
}

func outboundSyncThrough(now time.Time) time.Time {
	return now.In(time.Local).Truncate(outboundWindow)
}

func outboundSyncCursor(through time.Time, watermark *time.Time) time.Time {
	if watermark != nil {
		return watermark.In(time.Local).Add(-outboundWindow)
	}
	return through.Add(-outboundBootstrapLookback)
}

func withinOutboundCoverage(value, start, through *time.Time) bool {
	if value == nil || start == nil || through == nil {
		return false
	}
	return !value.Before(*start) && !value.After(*through)
}

func outboundAccountKey(credential model.WarehouseCredentials) string {
	digest := sha256.Sum256([]byte(strings.TrimRight(strings.TrimSpace(credential.APIBaseURL), "/") + "\x00" + strings.TrimSpace(credential.AppKey)))
	return hex.EncodeToString(digest[:])
}

func uniqueWarehouseCredentials(credentials []model.WarehouseCredentials) []model.WarehouseCredentials {
	unique := make([]model.WarehouseCredentials, 0, len(credentials))
	seen := make(map[string]struct{}, len(credentials))
	for _, credential := range credentials {
		key := outboundAccountKey(credential)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, credential)
	}
	return unique
}

func indexedOutboundOrder(record xlwms.OutboundOrderRecord) model.OutboundOrderIndex {
	return model.OutboundOrderIndex{
		WarehouseCode: strings.ToUpper(strings.TrimSpace(record.WarehouseCode)), OutboundOrderNo: strings.TrimSpace(record.OutboundOrderNo),
		ThirdOrderNo: strings.TrimSpace(record.ThirdOrderNo), ReferOrderNo: strings.TrimSpace(record.ReferOrderNo),
		PlatformOrderNo: strings.TrimSpace(record.PlatformOrderNo), Status: record.Status,
		TrackingNumber: strings.TrimSpace(record.TrackingNumber), OrderCreatedAt: parseXLWMSTime(record.OrderCreateTime),
		OutboundAt: parseXLWMSTime(record.OutboundTime),
	}
}

func matchIndexedOutboundRecords(items []model.FulfillmentAudit, records []model.OutboundOrderIndex) map[int64]model.OutboundOrderIndex {
	byReference := make(map[string][]model.OutboundOrderIndex)
	for _, record := range records {
		for _, reference := range []string{record.PlatformOrderNo, record.ThirdOrderNo, record.ReferOrderNo, record.OutboundOrderNo} {
			reference = strings.ToUpper(strings.TrimSpace(reference))
			if reference != "" {
				byReference[reference] = append(byReference[reference], record)
			}
		}
	}
	matches := make(map[int64]model.OutboundOrderIndex, len(items))
	for _, item := range items {
		candidates := byReference[strings.ToUpper(strings.TrimSpace(item.PlatformOrderNo))]
		if len(candidates) == 0 {
			continue
		}
		selected := candidates[0]
		warehouse := strings.ToUpper(strings.TrimSpace(item.WarehouseCode))
		for _, candidate := range candidates {
			if warehouse != "" && strings.EqualFold(candidate.WarehouseCode, warehouse) {
				selected = candidate
				break
			}
		}
		matches[item.ID] = selected
	}
	return matches
}

func resolutionFromRecord(record model.OutboundOrderIndex) model.FulfillmentAuditResolution {
	status := record.Status
	return model.FulfillmentAuditResolution{
		WarehouseCode: record.WarehouseCode, OMSStatus: normalizeOMSStatus(status), OMSStatusCode: &status,
		OMSOrderCreated: record.OrderCreatedAt, OMSOutboundAt: record.OutboundAt,
		OutboundOrderNo: record.OutboundOrderNo, TrackingNumber: record.TrackingNumber,
	}
}

func resolutionFromAudit(item model.FulfillmentAudit) model.FulfillmentAuditResolution {
	return model.FulfillmentAuditResolution{
		WarehouseCode: item.WarehouseCode, OMSStatus: item.OMSStatus, OMSStatusCode: item.OMSStatusCode,
		OMSOrderCreated: item.OMSOrderCreatedAt, OMSOutboundAt: item.OMSOutboundAt,
		OutboundOrderNo: item.OutboundOrderNo, TrackingNumber: item.OMSTrackingNumber,
	}
}

func normalizeOMSStatus(status int) string {
	switch status {
	case 0, 1:
		return "pending"
	case 2:
		return "processing"
	case 3:
		return "outbound"
	case 4, 5, 6, 7:
		return "exception"
	default:
		return "unknown"
	}
}

func parseXLWMSTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return &parsed
		}
	}
	return nil
}
