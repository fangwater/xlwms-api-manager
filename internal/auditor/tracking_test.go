package auditor

import (
	"errors"
	"testing"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/temutracking"
)

func TestTrackingResolutionClassifiesManifestOverdue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	outboundAt := now.Add(-25 * time.Hour)
	item := model.FulfillmentAudit{OMSOutboundAt: &outboundAt}
	tracking := temuTracking("Last-Mile Manifest", "waiting for carrier")

	resolution := trackingResolution(item, tracking, nil, now)

	if resolution.TrackingCategory != "pickup_exception" || resolution.PickupExceptionReason != "pickup_overdue" {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestTrackingResolutionClassifiesExplicitPickupFailureImmediately(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	outboundAt := now.Add(-2 * time.Hour)
	item := model.FulfillmentAudit{OMSOutboundAt: &outboundAt}
	tracking := temuTracking("Last Mile Carrier Pick up failed", "pickup failed")

	resolution := trackingResolution(item, tracking, nil, now)

	if resolution.TrackingCategory != "pickup_exception" || resolution.PickupExceptionReason != "pickup_failed" {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestTrackingResolutionRequiresEveryPackagePickedUp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	outboundAt := now.Add(-4 * time.Hour)
	tracking := temutracking.OrderTracking{Packages: []temutracking.Package{
		{PackageSN: "PKG-1", TrackingNum: "LM-1", TrackingInfo: []temutracking.Event{{LogisticsUpdatedAt: now.Add(-time.Hour).Format(time.RFC3339), LogisticsStatus: "Last Mile Carrier Picked up"}}},
		{PackageSN: "PKG-2", TrackingNum: "LM-2", TrackingInfo: []temutracking.Event{{LogisticsUpdatedAt: now.Add(-30 * time.Minute).Format(time.RFC3339), LogisticsStatus: "PICKED_UP"}}},
	}}

	resolution := trackingResolution(model.FulfillmentAudit{OMSOutboundAt: &outboundAt}, tracking, nil, now)

	if resolution.TrackingCategory != "picked_up" || resolution.TrackingPackageCount != 2 || resolution.PickedUpPackageCount != 2 {
		t.Fatalf("resolution = %+v", resolution)
	}
	if resolution.LastMileTrackingNumber != "LM-1, LM-2" {
		t.Fatalf("tracking numbers = %q", resolution.LastMileTrackingNumber)
	}
}

func TestTrackingResolutionClassifiesPartiallyPickedOrderOverdue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	outboundAt := now.Add(-24 * time.Hour)
	tracking := temutracking.OrderTracking{Packages: []temutracking.Package{
		{PackageSN: "PKG-1", TrackingInfo: []temutracking.Event{{LogisticsStatus: "Last Mile Carrier Picked up"}}},
		{PackageSN: "PKG-2", TrackingInfo: []temutracking.Event{{LogisticsStatus: "Last-Mile Manifest"}}},
	}}

	resolution := trackingResolution(model.FulfillmentAudit{OMSOutboundAt: &outboundAt}, tracking, nil, now)

	if resolution.TrackingCategory != "pickup_exception" || resolution.PickedUpPackageCount != 1 {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestTrackingResolutionKeepsQueryErrorsSeparateBeforeDeadline(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	outboundAt := now.Add(-4 * time.Hour)

	resolution := trackingResolution(model.FulfillmentAudit{OMSOutboundAt: &outboundAt}, temutracking.OrderTracking{}, errors.New("service unavailable"), now)

	if resolution.TrackingCategory != "tracking_error" || resolution.TrackingError == "" {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func temuTracking(status, text string) temutracking.OrderTracking {
	return temutracking.OrderTracking{Packages: []temutracking.Package{{
		PackageSN:    "PKG-1",
		TrackingNum:  "LM-1",
		TrackingInfo: []temutracking.Event{{LogisticsUpdatedAt: "2026-08-06T08:00:00Z", LogisticsStatus: status, StatusText: text}},
	}}}
}
