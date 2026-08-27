package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"xlwms-api-manager/internal/auditor"
	"xlwms-api-manager/internal/config"
	"xlwms-api-manager/internal/credentials"
	"xlwms-api-manager/internal/httpapi"
	"xlwms-api-manager/internal/oms"
	"xlwms-api-manager/internal/sheinfulfillment"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/syncer"
	"xlwms-api-manager/internal/temutracking"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("XLWMS service stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := requireLoopbackAddress(cfg.Listen); err != nil {
		return err
	}
	cipher, err := credentials.EnsureKeyFile(cfg.CredentialKeyFile)
	if err != nil {
		return err
	}
	destination, err := store.NewPostgres(ctx, cfg.DatabaseURL, cipher)
	if err != nil {
		return err
	}
	defer destination.Close()
	if err := destination.Migrate(ctx); err != nil {
		return err
	}
	if cfg.OMSUsername != "" {
		if err := destination.EnsureOMSAccount(ctx, "arp", cfg.OMSUsername, cfg.OMSPassword); err != nil {
			return fmt.Errorf("seed ARP OMS account: %w", err)
		}
	}
	service := syncer.New(ctx, destination, cfg.RequestTimeout, cfg.SyncTimeout, logger)
	go backgroundInventorySync(ctx, destination, service, cfg.InventorySyncInterval, logger)
	go backgroundCostSync(ctx, destination, service, cfg.CostSyncInterval, 2*cfg.SyncTimeout, logger)
	trackingClient := temutracking.NewClient(cfg.TemuGoBaseURL, cfg.RequestTimeout)
	platformSources := &platformOrderSources{
		Client: trackingClient,
		shein:  sheinfulfillment.NewClient(cfg.SheinGoBaseURL, cfg.RequestTimeout),
	}
	auditService := auditor.NewWithTracking(destination, trackingClient, cfg.RequestTimeout,
		cfg.FulfillmentTrackingLimit, cfg.FulfillmentTrackingWorkers, logger)
	var platformOrders *oms.Client
	if cfg.OMSUsername != "" {
		platformOrders = oms.NewClient(cfg.OMSBaseURL, cfg.OMSUsername, cfg.OMSPassword, cfg.RequestTimeout)
	}
	go backgroundFulfillmentAudits(ctx, auditService, cfg.FulfillmentAuditInterval, cfg.RequestTimeout*10, logger)
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	server := &http.Server{
		Handler:           httpapi.NewWithWarehousePlatformOrderOperations(destination, service, auditService, platformOrders, platformSources, cfg.OMSBaseURL, cfg.OMSUsername, cfg.OMSPassword, cfg.RequestTimeout, logger),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	serverErrors := make(chan error, 1)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serverErrors <- serveErr
		}
	}()
	logger.Info("XLWMS management API started", "listen", listener.Addr().String(), "inventory_endpoints", 7)
	select {
	case <-ctx.Done():
		logger.Info("XLWMS management API shutdown requested")
	case serveErr := <-serverErrors:
		return serveErr
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

type platformOrderSources struct {
	*temutracking.Client
	shein *sheinfulfillment.Client
}

func (s *platformOrderSources) PurchasedSheinLabelsByPlatformOrderNos(ctx context.Context, orderNos []string) (map[string]sheinfulfillment.PurchasedLabel, error) {
	return s.shein.PurchasedSheinLabelsByPlatformOrderNos(ctx, orderNos)
}

func backgroundFulfillmentAudits(ctx context.Context, service *auditor.Service, interval, timeout time.Duration, logger *slog.Logger) {
	run := func() {
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		stats, err := service.Check(checkCtx, 5000)
		if err != nil {
			logger.Warn("fulfillment audit check incomplete", "synced", stats.Synced, "checked", stats.Checked, "tracking_checked", stats.TrackingChecked, "tracking_failed", stats.TrackingFailed, "error", err)
		} else if stats.Checked > 0 || stats.Synced > 0 || stats.TrackingChecked > 0 {
			logger.Info("fulfillment audit check completed", "synced", stats.Synced, "checked", stats.Checked, "matched", stats.Matched, "missing", stats.Missing, "pending", stats.Pending, "failed", stats.Failed, "tracking_checked", stats.TrackingChecked, "pickup_exceptions", stats.PickupExceptions)
		}
	}
	run()
	for {
		timer := time.NewTimer(time.Until(nextFulfillmentAuditRun(time.Now(), interval)))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
			run()
		}
	}
}

func nextFulfillmentAuditRun(now time.Time, interval time.Duration) time.Time {
	if interval <= 0 {
		interval = time.Hour
	}
	return now.Truncate(interval).Add(interval)
}

func requireLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid XLWMS_LISTEN: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("XLWMS_LISTEN must use a loopback address")
	}
	return nil
}
