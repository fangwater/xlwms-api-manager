package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/store"
	"xlwms-api-manager/internal/xlwms"
)

var ErrAlreadyRunning = errors.New("a sync for this warehouse and target is already running")

const defaultStockFlowWindowDays = 30

type Service struct {
	ctx            context.Context
	store          *store.Postgres
	requestTimeout time.Duration
	syncTimeout    time.Duration
	logger         *slog.Logger
	mu             sync.Mutex
	running        map[string]struct{}
}

type DetailOptions struct {
	Workers           int
	RequestsPerSecond float64
	MaxAttempts       int
	Limit             int
}

func New(ctx context.Context, destination *store.Postgres, requestTimeout, syncTimeout time.Duration, logger *slog.Logger) *Service {
	return &Service{ctx: ctx, store: destination, requestTimeout: requestTimeout, syncTimeout: syncTimeout, logger: logger, running: make(map[string]struct{})}
}

func (s *Service) TriggerFundsFlow(warehouses []model.WarehouseCredentials, parameters map[string]any, pageSize int) ([]model.SyncRun, error) {
	if len(warehouses) == 0 {
		return nil, errors.New("no active warehouses configured")
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, errors.New("page_size must be between 1 and 100")
	}
	keys := make([]string, len(warehouses))
	for index, warehouse := range warehouses {
		keys[index] = warehouse.Code + ":funds_flow"
	}
	if err := s.reserve(keys); err != nil {
		return nil, err
	}
	runs := make([]model.SyncRun, 0, len(warehouses))
	for _, warehouse := range warehouses {
		run, err := s.store.StartSyncRun(s.ctx, warehouse.Code, "funds_flow")
		if err != nil {
			for _, created := range runs {
				s.finishRun(created, err)
			}
			s.release(keys)
			return nil, err
		}
		runs = append(runs, run)
	}
	for index, warehouse := range warehouses {
		go s.runFundsFlow(keys[index], runs[index], warehouse, cloneMap(parameters), pageSize)
	}
	return runs, nil
}

func (s *Service) TriggerCostDetails(warehouse model.WarehouseCredentials, options DetailOptions) (model.SyncRun, error) {
	if options.Workers < 1 || options.Workers > 32 {
		return model.SyncRun{}, errors.New("workers must be between 1 and 32")
	}
	if options.RequestsPerSecond <= 0 || options.RequestsPerSecond > 100 {
		return model.SyncRun{}, errors.New("requests_per_second must be greater than 0 and at most 100")
	}
	if options.MaxAttempts < 1 || options.MaxAttempts > 10 {
		return model.SyncRun{}, errors.New("max_attempts must be between 1 and 10")
	}
	key := warehouse.Code + ":cost_details"
	if err := s.reserve([]string{key}); err != nil {
		return model.SyncRun{}, err
	}
	run, err := s.store.StartSyncRun(s.ctx, warehouse.Code, "cost_details")
	if err != nil {
		s.release([]string{key})
		return model.SyncRun{}, err
	}
	go s.runCostDetails(key, run, warehouse, options)
	return run, nil
}

func (s *Service) TriggerInventory(warehouse model.WarehouseCredentials, kinds []string, parameters map[string]map[string]any, pageSize int) ([]model.SyncRun, error) {
	if len(kinds) == 0 {
		return nil, errors.New("at least one inventory kind is required")
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, errors.New("page_size must be between 1 and 100")
	}
	keys := make([]string, len(kinds))
	for index, kind := range kinds {
		if _, ok := xlwms.InventoryPaths[kind]; !ok {
			return nil, fmt.Errorf("unknown inventory kind: %s", kind)
		}
		keys[index] = warehouse.Code + ":" + kind
	}
	if err := s.reserve(keys); err != nil {
		return nil, err
	}
	runs := make([]model.SyncRun, 0, len(kinds))
	for _, kind := range kinds {
		run, err := s.store.StartSyncRun(s.ctx, warehouse.Code, kind)
		if err != nil {
			for _, created := range runs {
				s.finishRun(created, err)
			}
			s.release(keys)
			return nil, err
		}
		runs = append(runs, run)
	}
	for index, kind := range kinds {
		go s.runInventory(keys[index], runs[index], warehouse, kind, cloneMap(parameters[kind]), pageSize)
	}
	return runs, nil
}

func (s *Service) runFundsFlow(key string, run model.SyncRun, warehouse model.WarehouseCredentials, parameters map[string]any, pageSize int) {
	defer s.release([]string{key})
	ctx, cancel := context.WithTimeout(s.ctx, s.syncTimeout)
	defer cancel()
	client := xlwms.NewClient(warehouse.APIBaseURL, warehouse.AppKey, warehouse.AppSecret, s.requestTimeout)
	parameters["whCodeList"] = []any{warehouse.Code}
	records, pages, syncErr := fetchAll(ctx, func(page int) (map[string]any, error) { return client.PageFundsFlow(ctx, parameters, page, pageSize) })
	run.Pages, run.RecordsSeen = pages, len(records)
	if syncErr == nil {
		run.RecordsSaved, syncErr = s.store.ReplaceFundsFlowSnapshot(ctx, warehouse.Code, records)
	}
	s.finishRun(run, syncErr)
}

func (s *Service) runInventory(key string, run model.SyncRun, warehouse model.WarehouseCredentials, kind string, parameters map[string]any, pageSize int) {
	defer s.release([]string{key})
	ctx, cancel := context.WithTimeout(s.ctx, s.syncTimeout)
	defer cancel()
	client := xlwms.NewClient(warehouse.APIBaseURL, warehouse.AppKey, warehouse.AppSecret, s.requestTimeout)
	if kind == "integrated" {
		parameters["whCodeList"] = warehouse.Code
	} else {
		parameters["whCodeList"] = []any{warehouse.Code}
	}
	applyInventoryDefaults(kind, parameters, time.Now())

	var records []map[string]any
	var pages int
	var syncErr error
	if kind == "stock_age" {
		if _, specified := parameters["stockItemType"]; !specified {
			for _, stockItemType := range []int{0, 2} {
				query := cloneMap(parameters)
				query["stockItemType"] = stockItemType
				batch, batchPages, err := fetchAll(ctx, func(page int) (map[string]any, error) {
					return client.PageInventory(ctx, kind, query, page, pageSize)
				})
				records = append(records, batch...)
				pages += batchPages
				if err != nil {
					syncErr = err
					break
				}
			}
		} else {
			records, pages, syncErr = fetchAll(ctx, func(page int) (map[string]any, error) {
				return client.PageInventory(ctx, kind, parameters, page, pageSize)
			})
		}
	} else {
		records, pages, syncErr = fetchAll(ctx, func(page int) (map[string]any, error) {
			return client.PageInventory(ctx, kind, parameters, page, pageSize)
		})
	}
	run.Pages, run.RecordsSeen = pages, len(records)
	if syncErr == nil {
		run.RecordsSaved, syncErr = s.store.SaveInventoryRecords(ctx, kind, warehouse.Code, records)
	}
	s.finishRun(run, syncErr)
}

func applyInventoryDefaults(kind string, parameters map[string]any, now time.Time) {
	if kind != "stock_flow" || hasSyncParameter(parameters["startTime"]) || hasSyncParameter(parameters["endTime"]) {
		return
	}
	parameters["startTime"] = now.AddDate(0, 0, -(defaultStockFlowWindowDays - 1)).Format("2006-01-02")
	parameters["endTime"] = now.Format("2006-01-02")
}

func hasSyncParameter(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

type detailOutcome struct {
	target   store.DetailTarget
	attempts int
	detail   map[string]any
	code     string
	message  string
}

func (s *Service) runCostDetails(key string, run model.SyncRun, warehouse model.WarehouseCredentials, options DetailOptions) {
	defer s.release([]string{key})
	ctx, cancel := context.WithTimeout(s.ctx, s.syncTimeout)
	defer cancel()
	targets, syncErr := s.store.PendingDetailTargets(ctx, warehouse.Code, options.Limit)
	if syncErr != nil {
		s.finishRun(run, syncErr)
		return
	}
	run.Targets = len(targets)
	if len(targets) == 0 {
		s.finishRun(run, nil)
		return
	}
	client := xlwms.NewClient(warehouse.APIBaseURL, warehouse.AppKey, warehouse.AppSecret, s.requestTimeout)
	limiter := newRateLimiter(options.RequestsPerSecond)
	jobs := make(chan store.DetailTarget)
	results := make(chan detailOutcome)
	var workers sync.WaitGroup
	for index := 0; index < options.Workers; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for target := range jobs {
				results <- fetchDetail(ctx, client, limiter, target, options.MaxAttempts)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, target := range targets {
			select {
			case jobs <- target:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	for outcome := range results {
		if outcome.detail != nil {
			items, err := s.store.SaveCostDetail(ctx, warehouse.Code, outcome.target.OrderNo, 1, outcome.target.ModuleType, outcome.detail, outcome.attempts)
			if err != nil {
				syncErr = errors.Join(syncErr, err)
				run.Failed++
				continue
			}
			run.Succeeded++
			run.CostItems += items
		} else {
			if err := s.store.MarkCostDetailError(ctx, warehouse.Code, outcome.target, outcome.attempts, outcome.code, outcome.message); err != nil {
				syncErr = errors.Join(syncErr, err)
			}
			run.Failed++
		}
	}
	if ctx.Err() != nil {
		syncErr = errors.Join(syncErr, ctx.Err())
	}
	s.finishRun(run, syncErr)
}

func fetchAll(ctx context.Context, fetch func(int) (map[string]any, error)) ([]map[string]any, int, error) {
	records := make([]map[string]any, 0)
	pages := 1
	for page := 1; page <= pages; page++ {
		response, err := fetch(page)
		if err != nil {
			return records, pages, err
		}
		data, ok := response["data"].(map[string]any)
		if !ok {
			return records, pages, errors.New("paginated response is missing data")
		}
		pages = numberAsInt(data["pages"])
		if pages < 1 {
			total := numberAsInt(data["total"])
			pageSize := numberAsInt(data["pageSize"])
			if pageSize > 0 {
				pages = (total + pageSize - 1) / pageSize
			}
			if pages < 1 {
				pages = 1
			}
		}
		items, ok := data["records"].([]any)
		if !ok && data["records"] != nil {
			return records, pages, errors.New("paginated data.records must be an array")
		}
		for _, raw := range items {
			record, ok := raw.(map[string]any)
			if !ok {
				return records, pages, errors.New("paginated response contains an invalid record")
			}
			records = append(records, record)
		}
		select {
		case <-ctx.Done():
			return records, pages, ctx.Err()
		default:
		}
	}
	return records, pages, nil
}

func fetchDetail(ctx context.Context, client *xlwms.Client, limiter *rateLimiter, target store.DetailTarget, maxAttempts int) detailOutcome {
	result := detailOutcome{target: target}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result.attempts = attempt
		if err := limiter.Wait(ctx); err != nil {
			result.code, result.message = "cancelled", err.Error()
			return result
		}
		response, err := client.CostDetail(ctx, target.OrderNo, 1, target.ModuleType)
		if err == nil {
			detail, ok := response["data"].(map[string]any)
			costNo, valid := detail["costNo"].(string)
			if !ok || !valid || strings.TrimSpace(costNo) == "" {
				result.code, result.message = "invalid_response", "costDetail response is missing data.costNo"
				return result
			}
			result.detail = detail
			return result
		}
		result.code, result.message = "transport", err.Error()
		var apiErr *xlwms.APIError
		if errors.As(err, &apiErr) {
			result.code = apiErr.Code
		}
		if attempt < maxAttempts {
			timer := time.NewTimer(time.Duration(1<<(attempt-1)) * 500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				result.message = ctx.Err().Error()
				return result
			case <-timer.C:
			}
		}
	}
	return result
}

type rateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newRateLimiter(rps float64) *rateLimiter {
	return &rateLimiter{interval: time.Duration(float64(time.Second) / rps)}
}
func (l *rateLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	delay := l.next.Sub(now)
	if delay < 0 {
		delay = 0
	}
	if l.next.Before(now) {
		l.next = now
	}
	l.next = l.next.Add(l.interval)
	l.mu.Unlock()
	if delay == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) reserve(keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		if _, exists := s.running[key]; exists {
			return ErrAlreadyRunning
		}
	}
	for _, key := range keys {
		s.running[key] = struct{}{}
	}
	return nil
}
func (s *Service) release(keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		delete(s.running, key)
	}
}
func (s *Service) finishRun(run model.SyncRun, syncErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.store.FinishSyncRun(ctx, run, syncErr); err != nil {
		s.logger.Error("finish XLWMS sync run", "run_id", run.ID, "error", err)
	}
	s.logger.Info("XLWMS sync finished", "run_id", run.ID, "warehouse", run.WarehouseCode, "target", run.Target, "error", syncErr)
}
func numberAsInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	}
	return 0
}
func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}
