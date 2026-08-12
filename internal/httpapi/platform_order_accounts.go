package httpapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/oms"
)

type platformOrderAccountStore interface {
	ListWarehousesWithOMS(context.Context, bool) ([]model.WarehouseSummary, error)
	WarehouseOMSAccount(context.Context, string, bool) (model.WarehouseOMSAccount, error)
}

type platformOrderAccountSource interface {
	OperatorForWarehouse(context.Context, string) (platformOrderOperator, error)
}

type platformOrderAccount interface {
	platformOrderSource
	platformOrderOperator
}

type platformOrderAccountOption struct {
	Key            string   `json:"key"`
	Label          string   `json:"label"`
	WarehouseCodes []string `json:"warehouse_codes"`
}

type platformOrderAccountSelector interface {
	PlatformOrderAccounts(context.Context) ([]platformOrderAccountOption, error)
	OperatorForAccount(context.Context, string) (platformOrderAccount, error)
}

var (
	errPlatformOrderAccountNotFound    = errors.New("platform order account not found")
	errPlatformOrderAccountUnavailable = errors.New("platform order account is unavailable")
)

const defaultPlatformOrderAccountKey = "arp"
const dpsPlatformOrderAccountKey = "dps"
const platformOrderAccountHeader = "X-OMS-Account"

func requestedPlatformOrderAccount(request *http.Request) (string, error) {
	headerKey := strings.TrimSpace(request.Header.Get(platformOrderAccountHeader))
	queryKey := strings.TrimSpace(request.URL.Query().Get("account"))
	if headerKey != "" && queryKey != "" && !strings.EqualFold(headerKey, queryKey) {
		return "", errors.New("conflicting OMS account selectors")
	}
	if headerKey != "" {
		return headerKey, nil
	}
	if queryKey != "" {
		return queryKey, nil
	}
	return defaultPlatformOrderAccountKey, nil
}

type fixedPlatformOrderAccounts struct {
	operator platformOrderOperator
}

func (f fixedPlatformOrderAccounts) OperatorForWarehouse(context.Context, string) (platformOrderOperator, error) {
	return f.operator, nil
}

type postgresPlatformOrderAccounts struct {
	store          platformOrderAccountStore
	baseURL        string
	timeout        time.Duration
	shared         platformOrderAccount
	sharedUsername string
	sharedPassword string
	clientMu       sync.Mutex
	clients        map[[sha256.Size]byte]platformOrderAccount
}

func (p *postgresPlatformOrderAccounts) OperatorForWarehouse(ctx context.Context, warehouseCode string) (platformOrderOperator, error) {
	account, err := p.store.WarehouseOMSAccount(ctx, warehouseCode, true)
	if err != nil {
		return nil, err
	}
	return p.clientForCredentials(account.Username, account.Password), nil
}

func (p *postgresPlatformOrderAccounts) PlatformOrderAccounts(ctx context.Context) ([]platformOrderAccountOption, error) {
	accounts, err := p.selectableAccounts(ctx)
	if err != nil {
		return nil, err
	}
	options := make([]platformOrderAccountOption, 0, len(accounts))
	for _, account := range accounts {
		options = append(options, account.option)
	}
	return options, nil
}

func (p *postgresPlatformOrderAccounts) OperatorForAccount(ctx context.Context, key string) (platformOrderAccount, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.EqualFold(key, defaultPlatformOrderAccountKey) {
		if p.shared == nil {
			return nil, errPlatformOrderAccountUnavailable
		}
		return p.shared, nil
	}
	accounts, err := p.selectableAccounts(ctx)
	if err != nil {
		return nil, err
	}
	for _, selectable := range accounts {
		if !platformOrderAccountKeyMatches(selectable.option, key) {
			continue
		}
		if strings.EqualFold(selectable.option.Key, defaultPlatformOrderAccountKey) {
			if p.shared == nil {
				return nil, errPlatformOrderAccountUnavailable
			}
			return p.shared, nil
		}
		if selectable.warehouseCode == "" {
			return nil, errPlatformOrderAccountUnavailable
		}
		account, accountErr := p.store.WarehouseOMSAccount(ctx, selectable.warehouseCode, true)
		if accountErr != nil {
			return nil, accountErr
		}
		return p.clientForCredentials(account.Username, account.Password), nil
	}
	return nil, errPlatformOrderAccountNotFound
}

func platformOrderAccountKeyMatches(option platformOrderAccountOption, key string) bool {
	if strings.EqualFold(option.Key, key) {
		return true
	}
	if strings.EqualFold(key, dpsPlatformOrderAccountKey) {
		for _, code := range option.WarehouseCodes {
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(code)), "DPS") {
				return true
			}
		}
		return false
	}
	const warehousePrefix = "warehouse:"
	if !strings.HasPrefix(strings.ToLower(key), warehousePrefix) {
		return false
	}
	warehouseCode := strings.TrimSpace(key[len(warehousePrefix):])
	for _, code := range option.WarehouseCodes {
		if strings.EqualFold(code, warehouseCode) {
			return true
		}
	}
	return false
}

func (p *postgresPlatformOrderAccounts) clientForCredentials(username, password string) platformOrderAccount {
	fingerprint := platformOrderCredentialFingerprint(username, password)
	if p.shared != nil && p.sharedUsername != "" && p.sharedPassword != "" &&
		fingerprint == platformOrderCredentialFingerprint(p.sharedUsername, p.sharedPassword) {
		return p.shared
	}
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	if p.clients == nil {
		p.clients = make(map[[sha256.Size]byte]platformOrderAccount)
	}
	if client := p.clients[fingerprint]; client != nil {
		return client
	}
	client := oms.NewClient(p.baseURL, username, password, p.timeout)
	p.clients[fingerprint] = client
	return client
}

type selectablePlatformOrderAccount struct {
	option        platformOrderAccountOption
	warehouseCode string
}

type warehouseAccountGroup struct {
	warehouseCodes []string
}

func (p *postgresPlatformOrderAccounts) selectableAccounts(ctx context.Context) ([]selectablePlatformOrderAccount, error) {
	warehouses, err := p.store.ListWarehousesWithOMS(ctx, true)
	if err != nil {
		return nil, err
	}
	groups := make(map[[sha256.Size]byte]*warehouseAccountGroup)
	var sharedFingerprint [sha256.Size]byte
	sharedConfigured := p.shared != nil && p.sharedUsername != "" && p.sharedPassword != ""
	if sharedConfigured {
		sharedFingerprint = platformOrderCredentialFingerprint(p.sharedUsername, p.sharedPassword)
	}
	sharedWarehouseCodes := make([]string, 0)
	for _, warehouse := range warehouses {
		if !warehouse.OMSAccountConfigured {
			continue
		}
		account, accountErr := p.store.WarehouseOMSAccount(ctx, warehouse.Code, true)
		if accountErr != nil {
			return nil, accountErr
		}
		fingerprint := platformOrderCredentialFingerprint(account.Username, account.Password)
		if sharedConfigured && fingerprint == sharedFingerprint {
			sharedWarehouseCodes = append(sharedWarehouseCodes, warehouse.Code)
			continue
		}
		group := groups[fingerprint]
		if group == nil {
			group = &warehouseAccountGroup{}
			groups[fingerprint] = group
		}
		group.warehouseCodes = append(group.warehouseCodes, warehouse.Code)
	}

	accounts := make([]selectablePlatformOrderAccount, 0, len(groups)+1)
	if p.shared != nil {
		sort.Strings(sharedWarehouseCodes)
		accounts = append(accounts, selectablePlatformOrderAccount{option: platformOrderAccountOption{
			Key: defaultPlatformOrderAccountKey, Label: "ARP 账户", WarehouseCodes: sharedWarehouseCodes,
		}})
	}
	for _, group := range groups {
		sort.Strings(group.warehouseCodes)
		warehouseCode := group.warehouseCodes[0]
		accounts = append(accounts, selectablePlatformOrderAccount{
			option: platformOrderAccountOption{
				Key:            "warehouse:" + warehouseCode,
				Label:          platformOrderAccountLabel(group.warehouseCodes),
				WarehouseCodes: group.warehouseCodes,
			},
			warehouseCode: warehouseCode,
		})
	}
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].option.Key == defaultPlatformOrderAccountKey {
			return accounts[j].option.Key != defaultPlatformOrderAccountKey
		}
		if accounts[j].option.Key == defaultPlatformOrderAccountKey {
			return false
		}
		if accounts[i].option.Label != accounts[j].option.Label {
			return accounts[i].option.Label < accounts[j].option.Label
		}
		return accounts[i].option.Key < accounts[j].option.Key
	})
	return accounts, nil
}

func platformOrderCredentialFingerprint(username, password string) [sha256.Size]byte {
	return sha256.Sum256([]byte(strings.TrimSpace(username) + "\x00" + password))
}

func platformOrderAccountLabel(warehouseCodes []string) string {
	if len(warehouseCodes) == 0 {
		return "OMS 账户"
	}
	prefix := leadingWarehouseLetters(warehouseCodes[0])
	for _, code := range warehouseCodes[1:] {
		prefix = commonPrefix(prefix, leadingWarehouseLetters(code))
	}
	if len(prefix) < 2 {
		prefix = warehouseCodes[0]
	}
	return strings.ToUpper(prefix) + " 账户"
}

func leadingWarehouseLetters(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	for index, character := range value {
		if character < 'A' || character > 'Z' {
			return value[:index]
		}
	}
	return value
}

func commonPrefix(left, right string) string {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	index := 0
	for index < limit && left[index] == right[index] {
		index++
	}
	return left[:index]
}

func (s *Server) selectedPlatformOrderAccount(ctx context.Context, key string) (platformOrderAccount, error) {
	if selector, ok := s.platformAccounts.(platformOrderAccountSelector); ok {
		return selector.OperatorForAccount(ctx, key)
	}
	key = strings.TrimSpace(key)
	if key != "" && !strings.EqualFold(key, defaultPlatformOrderAccountKey) {
		return nil, errPlatformOrderAccountNotFound
	}
	account, ok := s.platformOrders.(platformOrderAccount)
	if !ok || !platformOrderAccountAvailable(account) {
		return nil, errPlatformOrderAccountUnavailable
	}
	return account, nil
}

func platformOrderAccountAvailable(account platformOrderAccount) bool {
	if account == nil {
		return false
	}
	value := reflect.ValueOf(account)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func (s *Server) availablePlatformOrderAccounts(ctx context.Context) ([]platformOrderAccountOption, error) {
	if selector, ok := s.platformAccounts.(platformOrderAccountSelector); ok {
		return selector.PlatformOrderAccounts(ctx)
	}
	if _, err := s.selectedPlatformOrderAccount(ctx, defaultPlatformOrderAccountKey); err != nil {
		return nil, err
	}
	return []platformOrderAccountOption{{
		Key: defaultPlatformOrderAccountKey, Label: "ARP 账户", WarehouseCodes: []string{},
	}}, nil
}

func (s *Server) listPlatformOrderAccounts(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	accounts, err := s.availablePlatformOrderAccounts(ctx)
	if err != nil {
		s.logger.Warn("list selectable OMS platform order accounts", "error", err)
		writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "无法读取 OMS 账户列表"})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: accounts})
}

func writePlatformOrderAccountError(writer http.ResponseWriter, err error) {
	if errors.Is(err, errPlatformOrderAccountNotFound) {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户无效或已停用"})
		return
	}
	writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "所选 OMS 账户暂不可用"})
}
