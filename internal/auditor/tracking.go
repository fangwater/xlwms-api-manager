package auditor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/temutracking"
)

const (
	pickupFailureGracePeriod = 12 * time.Hour
	pickupOverdueAfter       = 24 * time.Hour
)

type trackingSource interface {
	OrderTracking(context.Context, string, string) (temutracking.OrderTracking, error)
}

type trackingCheckStats struct {
	checked    int
	failed     int
	exceptions int
}

type trackingCheckResult struct {
	failed    bool
	exception bool
	err       error
}

func (s *Service) finishTrackingCheck(ctx context.Context, stats CheckStats, auditErr error, now time.Time) (CheckStats, error) {
	trackingStats, trackingErr := s.checkFulfillmentTracking(ctx, now)
	stats.TrackingChecked = trackingStats.checked
	stats.TrackingFailed = trackingStats.failed
	stats.PickupExceptions = trackingStats.exceptions
	categoryErr := s.store.RefreshFulfillmentTrackingCategories(ctx)
	return stats, errors.Join(auditErr, trackingErr, categoryErr)
}

func (s *Service) checkFulfillmentTracking(ctx context.Context, now time.Time) (trackingCheckStats, error) {
	var stats trackingCheckStats
	if s.tracking == nil {
		return stats, nil
	}
	limit := s.trackingLimit
	if limit < 1 || limit > 5000 {
		limit = 500
	}
	exceptionStats, exceptionErr := s.checkFulfillmentTrackingQueue(ctx, now, store.FulfillmentTrackingQueuePickupException, limit)
	regularStats, regularErr := s.checkFulfillmentTrackingQueue(ctx, now, store.FulfillmentTrackingQueueRegular, limit)
	stats.checked = exceptionStats.checked + regularStats.checked
	stats.failed = exceptionStats.failed + regularStats.failed
	stats.exceptions = exceptionStats.exceptions + regularStats.exceptions
	return stats, errors.Join(exceptionErr, regularErr)
}

func (s *Service) checkFulfillmentTrackingQueue(ctx context.Context, now time.Time, queueName string, limit int) (trackingCheckStats, error) {
	var stats trackingCheckStats
	batch, err := s.store.FulfillmentTrackingCandidates(ctx, queueName, limit)
	if err != nil || len(batch.Items) == 0 {
		return stats, err
	}
	workers := s.trackingWorkers
	if workers < 1 {
		workers = 1
	}
	if workers > 32 {
		workers = 32
	}
	if workers > len(batch.Items) {
		workers = len(batch.Items)
	}

	jobs := make(chan model.FulfillmentAudit, len(batch.Items))
	results := make(chan trackingCheckResult, len(batch.Items))
	for _, item := range batch.Items {
		jobs <- item
	}
	close(jobs)

	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for item := range jobs {
				tracking, queryErr := s.tracking.OrderTracking(ctx, item.ShopCode, item.PlatformOrderNo)
				resolution := trackingResolution(item, tracking, queryErr, now)
				updateErr := s.store.UpdateFulfillmentTrackingResolution(ctx, item.ID, resolution)
				results <- trackingCheckResult{
					failed:    queryErr != nil,
					exception: resolution.TrackingCategory == "pickup_exception",
					err:       updateErr,
				}
			}
		}()
	}
	group.Wait()
	close(results)

	var updateErrors []error
	for result := range results {
		stats.checked++
		if result.failed {
			stats.failed++
		}
		if result.exception {
			stats.exceptions++
		}
		if result.err != nil {
			updateErrors = append(updateErrors, result.err)
		}
	}
	watermarkCanAdvance := len(updateErrors) == 0
	if stats.failed > 0 {
		updateErrors = append(updateErrors, fmt.Errorf("%d Temu tracking queries failed", stats.failed))
	}
	if watermarkCanAdvance {
		if err := s.store.AdvanceFulfillmentTrackingWatermark(ctx, batch, stats.failed); err != nil {
			updateErrors = append(updateErrors, err)
		}
	}
	return stats, errors.Join(updateErrors...)
}

func trackingResolution(item model.FulfillmentAudit, tracking temutracking.OrderTracking, queryErr error, now time.Time) model.FulfillmentTrackingResolution {
	resolution := model.FulfillmentTrackingResolution{
		LastMileTrackingNumber: item.LastMileTrackingNumber,
		TrackingStatus:         item.TrackingStatus,
		TrackingStatusText:     item.TrackingStatusText,
		TrackingUpdatedAt:      item.TrackingUpdatedAt,
		TrackingCategory:       item.TrackingCategory,
		TrackingPackageCount:   item.TrackingPackageCount,
		PickedUpPackageCount:   item.PickedUpPackageCount,
		PickupExceptionReason:  item.PickupExceptionReason,
		PickupConfirmedAt:      item.PickupConfirmedAt,
	}
	if queryErr != nil {
		resolution.TrackingError = trackingErrorMessage(queryErr)
		classifyTracking(&resolution, item, resolution.PickupExceptionReason == "pickup_failed", now)
		return resolution
	}

	resolution.TrackingError = ""
	resolution.TrackingPackageCount = len(tracking.Packages)
	resolution.PickedUpPackageCount = 0
	resolution.PickupConfirmedAt = nil
	trackingNumbers := make(map[string]struct{})
	var latestEvent temutracking.Event
	var latestAt *time.Time
	var latestFallback bool
	var latestPickupAt *time.Time
	pickupFailed := false
	for _, pkg := range tracking.Packages {
		if value := strings.TrimSpace(pkg.TrackingNum); value != "" {
			trackingNumbers[value] = struct{}{}
		}
		packagePickedUp := false
		for _, event := range pkg.TrackingInfo {
			status := normalizeTrackingStatus(event.LogisticsStatus)
			eventAt := parseTrackingTime(event.LogisticsUpdatedAt)
			if eventAt != nil && (latestAt == nil || eventAt.After(*latestAt)) {
				value := *eventAt
				latestAt = &value
				latestEvent = event
				latestFallback = true
			} else if latestAt == nil && !latestFallback {
				latestEvent = event
				latestFallback = true
			}
			if trackingStatusConfirmsPickup(status) {
				packagePickedUp = true
			}
			if trackingStatusPickedUp(status) {
				if eventAt != nil && (latestPickupAt == nil || eventAt.After(*latestPickupAt)) {
					value := *eventAt
					latestPickupAt = &value
				}
			}
			if trackingStatusPickupFailed(status) {
				pickupFailed = true
			}
		}
		if packagePickedUp {
			resolution.PickedUpPackageCount++
		}
	}
	numbers := make([]string, 0, len(trackingNumbers))
	for value := range trackingNumbers {
		numbers = append(numbers, value)
	}
	sort.Strings(numbers)
	resolution.LastMileTrackingNumber = strings.Join(numbers, ", ")
	resolution.TrackingStatus = strings.TrimSpace(latestEvent.LogisticsStatus)
	resolution.TrackingStatusText = strings.TrimSpace(latestEvent.StatusText)
	resolution.TrackingUpdatedAt = latestAt
	if resolution.TrackingPackageCount > 0 && resolution.PickedUpPackageCount >= resolution.TrackingPackageCount {
		resolution.PickupConfirmedAt = latestPickupAt
	}
	classifyTracking(&resolution, item, pickupFailed, now)
	return resolution
}

func classifyTracking(resolution *model.FulfillmentTrackingResolution, item model.FulfillmentAudit, pickupFailed bool, now time.Time) {
	if resolution.TrackingPackageCount > 0 && resolution.PickedUpPackageCount >= resolution.TrackingPackageCount {
		resolution.TrackingCategory = "picked_up"
		resolution.PickupExceptionReason = ""
		return
	}
	if pickupFailed && pickupFailureGraceElapsed(item, now) {
		resolution.TrackingCategory = "pickup_exception"
		resolution.PickupExceptionReason = "pickup_failed"
		return
	}
	if trackingOverdue(item, now) {
		resolution.TrackingCategory = "pickup_exception"
		resolution.PickupExceptionReason = "pickup_overdue"
		return
	}
	if pickupFailed {
		resolution.PickupExceptionReason = "pickup_failed"
	} else {
		resolution.PickupExceptionReason = ""
	}
	if resolution.TrackingError != "" {
		resolution.TrackingCategory = "tracking_error"
	} else {
		resolution.TrackingCategory = "awaiting_pickup"
	}
}

func pickupFailureGraceElapsed(item model.FulfillmentAudit, now time.Time) bool {
	started := trackingStartedAt(item)
	return started == nil || !started.After(now.Add(-pickupFailureGracePeriod))
}

func trackingOverdue(item model.FulfillmentAudit, now time.Time) bool {
	started := trackingStartedAt(item)
	return started != nil && !started.After(now.Add(-pickupOverdueAfter))
}

func trackingStartedAt(item model.FulfillmentAudit) *time.Time {
	if item.OMSOutboundAt != nil {
		return item.OMSOutboundAt
	}
	return item.PlatformShippingAt
}

func normalizeTrackingStatus(value string) string {
	value = strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(strings.TrimSpace(value)))
	return strings.Join(strings.Fields(value), " ")
}

func trackingStatusPickedUp(status string) bool {
	return status == "last mile carrier picked up" || status == "picked up"
}

func trackingStatusConfirmsPickup(status string) bool {
	return trackingStatusPickedUp(status) || status == "delivered" ||
		status == "in transit" || strings.HasPrefix(status, "in transit ")
}

func trackingStatusPickupFailed(status string) bool {
	return status == "last mile carrier pick up failed" || status == "pickup failed"
}

func parseTrackingTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if timestamp, err := strconv.ParseInt(value, 10, 64); err == nil {
		if timestamp > 1_000_000_000_000 {
			timestamp /= 1000
		}
		if timestamp > 0 {
			parsed := time.Unix(timestamp, 0)
			return &parsed
		}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return &parsed
		}
	}
	return nil
}

func trackingErrorMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 200 {
		message = message[:200]
	}
	return message
}
