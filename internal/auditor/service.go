package auditor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/xlwms"
)

type Service struct {
	store           *store.Postgres
	requestTimeout  time.Duration
	logger          *slog.Logger
	tracking        trackingSource
	trackingLimit   int
	trackingWorkers int
}

type CheckStats struct {
	Checked          int `json:"checked"`
	Matched          int `json:"matched"`
	Missing          int `json:"missing"`
	Pending          int `json:"pending"`
	Failed           int `json:"failed"`
	Synced           int `json:"synced"`
	TrackingChecked  int `json:"tracking_checked"`
	TrackingFailed   int `json:"tracking_failed"`
	PickupExceptions int `json:"pickup_exceptions"`
}

const (
	outboundPageSize        = 50
	outboundMaxPages        = 500
	outboundWindow          = time.Hour
	outboundCoverageLimit   = 5000
	outboundStatusBatchSize = 50
	outboundDetailBatchSize = 50
	// A new installation backfills once, then advances only from its persisted watermark.
	outboundBootstrapLookback = 24 * time.Hour
)

func New(destination *store.Postgres, requestTimeout time.Duration, logger *slog.Logger) *Service {
	return NewWithTracking(destination, nil, requestTimeout, 500, 8, logger)
}

func NewWithTracking(destination *store.Postgres, tracking trackingSource, requestTimeout time.Duration, limit, workers int, logger *slog.Logger) *Service {
	return &Service{store: destination, requestTimeout: requestTimeout, logger: logger, tracking: tracking, trackingLimit: limit, trackingWorkers: workers}
}

func (s *Service) Check(ctx context.Context, limit int) (CheckStats, error) {
	stats := CheckStats{}
	credentials, err := s.activeWarehouseCredentials(ctx)
	if err != nil {
		return stats, err
	}
	now := time.Now()
	through := outboundSyncThrough(now)
	synced, forwardErr := s.syncOutboundOrders(ctx, now, credentials)
	backfilled, historicalErr := s.syncRequiredOutboundWindows(ctx, through, credentials)
	stats.Synced = synced + backfilled
	syncErr := errors.Join(forwardErr, historicalErr)

	statusCandidates, err := s.store.FulfillmentAuditStatusCandidates(ctx, limit)
	if err != nil {
		return stats, errors.Join(syncErr, err)
	}
	statusSynced, statusErr := s.refreshMatchedOutboundStatuses(ctx, statusCandidates, credentials)
	stats.Synced += statusSynced
	syncErr = errors.Join(syncErr, statusErr)

	candidates, err := s.store.FulfillmentAuditCandidates(ctx, limit)
	if err != nil {
		return stats, errors.Join(syncErr, err)
	}
	candidates = mergeFulfillmentCandidates(candidates, statusCandidates)
	stats.Checked = len(candidates)
	if len(candidates) == 0 {
		return s.finishTrackingCheck(ctx, stats, syncErr, now)
	}
	references := make([]string, 0, len(candidates))
	for _, item := range candidates {
		references = append(references, item.PlatformOrderNo)
	}
	records, err := s.store.OutboundOrdersByReferences(ctx, references)
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
		} else if item.OutboundOrderNo != "" && item.OMSStatus != "not_found" && item.OMSStatus != "pending_query" && item.OMSStatus != "query_error" {
			resolution = resolutionFromAudit(item)
		} else if item.PlatformShippingAt != nil && item.PlatformShippingAt.Before(through) {
			resolution = model.FulfillmentAuditResolution{OMSStatus: "not_found"}
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
	return s.finishTrackingCheck(ctx, stats, syncErr, now)
}

func mergeFulfillmentCandidates(groups ...[]model.FulfillmentAudit) []model.FulfillmentAudit {
	seen := make(map[int64]struct{})
	merged := make([]model.FulfillmentAudit, 0)
	for _, group := range groups {
		for _, item := range group {
			if _, exists := seen[item.ID]; exists {
				continue
			}
			seen[item.ID] = struct{}{}
			merged = append(merged, item)
		}
	}
	return merged
}

func (s *Service) refreshMatchedOutboundStatuses(ctx context.Context, items []model.FulfillmentAudit, credentials []model.WarehouseCredentials) (int, error) {
	seen := make(map[string]struct{}, len(items))
	orderNos := make([]string, 0, len(items))
	for _, item := range items {
		orderNo := strings.TrimSpace(item.OutboundOrderNo)
		if orderNo == "" || item.OMSStatus == "outbound" {
			continue
		}
		key := strings.ToUpper(orderNo)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		orderNos = append(orderNos, orderNo)
	}
	if len(orderNos) == 0 {
		return 0, nil
	}
	total := 0
	var queryErrors []error
	for _, credential := range credentials {
		records, statusErr := s.fetchOutboundStatuses(ctx, credential, orderNos)
		if statusErr != nil {
			queryErrors = append(queryErrors, fmt.Errorf("refresh outbound statuses for %s: %w", credential.Code, statusErr))
			continue
		}
		references := outboundSiblingReferences(records)
		if len(references) > 0 {
			siblings, siblingErr := s.fetchOutboundSiblingDetails(ctx, credential, references)
			if siblingErr != nil {
				queryErrors = append(queryErrors, fmt.Errorf("refresh outbound siblings for %s: %w", credential.Code, siblingErr))
			} else {
				records = append(records, siblings...)
			}
		}
		records = uniqueOutboundRecords(records)
		if err := s.store.SaveOutboundStatusRecords(ctx, outboundAccountKey(credential), records); err != nil {
			queryErrors = append(queryErrors, err)
			continue
		}
		total += len(records)
	}
	return total, errors.Join(queryErrors...)
}

func (s *Service) fetchOutboundStatuses(ctx context.Context, credential model.WarehouseCredentials, orderNos []string) ([]model.OutboundOrderIndex, error) {
	client := xlwms.NewClient(credential.APIBaseURL, credential.AppKey, credential.AppSecret, s.requestTimeout)
	records := make([]model.OutboundOrderIndex, 0, len(orderNos))
	for start := 0; start < len(orderNos); start += outboundStatusBatchSize {
		end := start + outboundStatusBatchSize
		if end > len(orderNos) {
			end = len(orderNos)
		}
		for page := 1; page <= outboundMaxPages; page++ {
			result, err := client.Outbound(ctx, "parcel-list", map[string]any{
				"page": page, "pageSize": outboundPageSize,
				"outboundOrderNos": strings.Join(orderNos[start:end], ","),
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
				break
			}
		}
	}
	return records, nil
}

func (s *Service) fetchOutboundSiblingDetails(ctx context.Context, credential model.WarehouseCredentials, references []string) ([]model.OutboundOrderIndex, error) {
	client := xlwms.NewClient(credential.APIBaseURL, credential.AppKey, credential.AppSecret, s.requestTimeout)
	records := make([]model.OutboundOrderIndex, 0, len(references))
	for start := 0; start < len(references); start += outboundDetailBatchSize {
		end := start + outboundDetailBatchSize
		if end > len(references) {
			end = len(references)
		}
		result, err := client.Outbound(ctx, "parcel-detail", map[string]any{
			"referOrderNoList": references[start:end],
		})
		if err != nil {
			return nil, err
		}
		detailRecords, err := xlwms.OutboundOrderRecords(result)
		if err != nil {
			return nil, err
		}
		for _, record := range detailRecords {
			records = append(records, indexedOutboundOrder(record))
		}
	}
	return records, nil
}

func outboundSiblingReferences(records []model.OutboundOrderIndex) []string {
	seen := make(map[string]struct{}, len(records))
	references := make([]string, 0, len(records))
	for _, record := range records {
		reference := strings.TrimSpace(record.ReferOrderNo)
		if reference == "" {
			continue
		}
		key := strings.ToUpper(reference)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		references = append(references, reference)
	}
	return references
}

func uniqueOutboundRecords(records []model.OutboundOrderIndex) []model.OutboundOrderIndex {
	seen := make(map[string]int, len(records))
	unique := make([]model.OutboundOrderIndex, 0, len(records))
	for _, record := range records {
		key := strings.ToUpper(strings.TrimSpace(record.OutboundOrderNo))
		if key == "" {
			continue
		}
		if index, exists := seen[key]; exists {
			unique[index] = record
			continue
		}
		seen[key] = len(unique)
		unique = append(unique, record)
	}
	return unique
}

func (s *Service) ReconcileReferences(ctx context.Context, references []string) (int, error) {
	items, err := s.store.FulfillmentAuditsByPlatformOrderNos(ctx, references)
	if err != nil || len(items) == 0 {
		return 0, err
	}
	records, err := s.store.OutboundOrdersByReferences(ctx, references)
	if err != nil {
		return 0, err
	}
	matches := matchIndexedOutboundRecords(items, records)
	for id, record := range matches {
		if err := s.store.UpdateFulfillmentAuditResolution(ctx, id, resolutionFromRecord(record)); err != nil {
			return 0, err
		}
	}
	if err := s.store.RefreshFulfillmentAuditCategories(ctx); err != nil {
		return 0, err
	}
	return len(matches), nil
}

func (s *Service) activeWarehouseCredentials(ctx context.Context) ([]model.WarehouseCredentials, error) {
	credentials, err := s.store.ActiveWarehouseCredentials(ctx)
	if err != nil {
		return nil, err
	}
	credentials = uniqueWarehouseCredentials(credentials)
	if len(credentials) == 0 {
		return nil, errors.New("no active XLWMS warehouse credentials")
	}
	return credentials, nil
}

func (s *Service) syncOutboundOrders(ctx context.Context, now time.Time, credentials []model.WarehouseCredentials) (int, error) {
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

func (s *Service) syncRequiredOutboundWindows(ctx context.Context, through time.Time, credentials []model.WarehouseCredentials) (int, error) {
	hours, err := s.store.FulfillmentAuditCoverageHours(ctx, through, outboundCoverageLimit)
	if err != nil {
		return 0, err
	}
	hours = expandOutboundCoverageHours(hours, through, outboundCoverageLimit)
	total := 0
	var syncErrors []error
	for _, hour := range hours {
		for _, credential := range credentials {
			accountKey := outboundAccountKey(credential)
			covered, err := s.store.OutboundWindowCovered(ctx, accountKey, hour)
			if err != nil {
				syncErrors = append(syncErrors, err)
				continue
			}
			if covered {
				continue
			}
			records, fetchErr := s.fetchOutboundWindow(ctx, credential, hour, hour.Add(outboundWindow))
			if fetchErr != nil {
				_ = s.store.MarkOutboundSyncFailure(context.WithoutCancel(ctx), accountKey, credential.Code, fetchErr)
				syncErrors = append(syncErrors, fmt.Errorf("backfill outbound account %s at %s: %w", credential.Code, hour.Format("2006-01-02 15:04:05"), fetchErr))
				continue
			}
			if err := s.store.SaveOutboundSyncWindow(ctx, accountKey, credential.Code, hour, hour.Add(outboundWindow), records); err != nil {
				syncErrors = append(syncErrors, err)
				continue
			}
			s.logger.Info("outbound historical window indexed", "warehouse", credential.Code,
				"start", hour.Format("2006-01-02 15:04:05"), "records", len(records))
			total += len(records)
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

func expandOutboundCoverageHours(base []time.Time, through time.Time, limit int) []time.Time {
	seen := make(map[time.Time]struct{}, len(base)*3)
	hours := make([]time.Time, 0, len(base)*3)
	for _, hour := range base {
		hour = hour.In(time.Local).Truncate(outboundWindow)
		for offset := -outboundWindow; offset <= outboundWindow; offset += outboundWindow {
			candidate := hour.Add(offset)
			if !candidate.Before(through) {
				continue
			}
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			hours = append(hours, candidate)
		}
	}
	sort.Slice(hours, func(left, right int) bool { return hours[left].After(hours[right]) })
	if limit > 0 && len(hours) > limit {
		return hours[:limit]
	}
	return hours
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
		for _, candidate := range candidates[1:] {
			if preferOutboundRecord(candidate, selected, warehouse) {
				selected = candidate
			}
		}
		matches[item.ID] = selected
	}
	return matches
}

func preferOutboundRecord(candidate, selected model.OutboundOrderIndex, warehouse string) bool {
	if warehouse != "" {
		candidateMatches := strings.EqualFold(candidate.WarehouseCode, warehouse)
		selectedMatches := strings.EqualFold(selected.WarehouseCode, warehouse)
		if candidateMatches != selectedMatches {
			return candidateMatches
		}
	}
	return outboundStatusPriority(candidate.Status) > outboundStatusPriority(selected.Status)
}

func outboundStatusPriority(status int) int {
	switch status {
	case 3:
		return 4
	case 2:
		return 3
	case 0, 1:
		return 2
	case 4, 5, 6, 7:
		return 0
	default:
		return 1
	}
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
