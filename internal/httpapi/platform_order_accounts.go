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

	"xlwms-api-manager/internal/credentials"
	"xlwms-api-manager/internal/model"
	"xlwms-api-manager/internal/oms"
	"xlwms-api-manager/internal/store"
)

type platformOrderAccountStore interface {
	ListWarehousesWithOMS(context.Context, bool) ([]model.WarehouseSummary, error)
	WarehouseOMSAccount(context.Context, string, bool) (model.WarehouseOMSAccount, error)
	SetWarehouseOMSAccount(context.Context, string, string, string) (model.WarehouseSummary, error)
	OMSAccount(context.Context, string) (model.OMSLoginAccount, error)
	SetOMSAccount(context.Context, string, string, string) (model.OMSLoginAccount, error)
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
	UsernameHint   string   `json:"username_hint,omitempty"`
	Available      bool     `json:"available"`
	Status         string   `json:"status,omitempty"`
	Error          string   `json:"error,omitempty"`
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
	return requestedPlatformOrderAccountWithBody(request, "")
}

func requestedPlatformOrderAccountWithBody(request *http.Request, bodyKey string) (string, error) {
	headerKey := strings.TrimSpace(request.Header.Get(platformOrderAccountHeader))
	queryKey := strings.TrimSpace(request.URL.Query().Get("account"))
	bodyKey = strings.TrimSpace(bodyKey)
	selected := ""
	for _, key := range []string{headerKey, queryKey, bodyKey} {
		if key == "" {
			continue
		}
		if selected != "" && !strings.EqualFold(selected, key) {
			return "", errors.New("conflicting OMS account selectors")
		}
		selected = key
	}
	if selected != "" {
		return selected, nil
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
		account, _, _, err := p.resolveShared(ctx)
		return account, err
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
			account, _, _, sharedErr := p.resolveShared(ctx)
			return account, sharedErr
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

func (p *postgresPlatformOrderAccounts) resolveShared(ctx context.Context) (platformOrderAccount, string, string, error) {
	stored, err := p.store.OMSAccount(ctx, defaultPlatformOrderAccountKey)
	if err == nil {
		return p.clientForCredentials(stored.Username, stored.Password), stored.Username, stored.Password, nil
	}
	if !errors.Is(err, store.ErrOMSAccountNotFound) {
		return nil, "", "", err
	}
	if p.shared == nil {
		return nil, "", "", errPlatformOrderAccountUnavailable
	}
	return p.shared, p.sharedUsername, p.sharedPassword, nil
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
	_, sharedUsername, sharedPassword, sharedErr := p.resolveShared(ctx)
	sharedConfigured := sharedErr == nil && sharedUsername != "" && sharedPassword != ""
	var sharedFingerprint [sha256.Size]byte
	if sharedConfigured {
		sharedFingerprint = platformOrderCredentialFingerprint(sharedUsername, sharedPassword)
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
	if sharedErr == nil || p.shared != nil {
		sort.Strings(sharedWarehouseCodes)
		accounts = append(accounts, selectablePlatformOrderAccount{option: platformOrderAccountOption{
			Key: defaultPlatformOrderAccountKey, Label: "ARP 账户", WarehouseCodes: sharedWarehouseCodes,
			UsernameHint: credentials.MaskIdentifier(sharedUsername),
		}})
	}
	for _, group := range groups {
		sort.Strings(group.warehouseCodes)
		warehouseCode := group.warehouseCodes[0]
		account, accountErr := p.store.WarehouseOMSAccount(ctx, warehouseCode, true)
		usernameHint := ""
		if accountErr == nil {
			usernameHint = credentials.MaskIdentifier(account.Username)
		}
		accounts = append(accounts, selectablePlatformOrderAccount{
			option: platformOrderAccountOption{
				Key:            "warehouse:" + warehouseCode,
				Label:          platformOrderAccountLabel(group.warehouseCodes),
				WarehouseCodes: group.warehouseCodes,
				UsernameHint:   usernameHint,
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
	var accounts []platformOrderAccountOption
	var err error
	if selector, ok := s.platformAccounts.(platformOrderAccountSelector); ok {
		accounts, err = selector.PlatformOrderAccounts(ctx)
	} else {
		if _, err = s.selectedPlatformOrderAccount(ctx, defaultPlatformOrderAccountKey); err != nil {
			return nil, err
		}
		accounts = []platformOrderAccountOption{{
			Key: defaultPlatformOrderAccountKey, Label: "ARP 账户", WarehouseCodes: []string{},
		}}
	}
	if err != nil {
		return nil, err
	}
	s.annotatePlatformOrderAccountHealth(ctx, accounts)
	return accounts, nil
}

type platformOrderAccessChecker interface {
	CheckAccess(context.Context) error
}

func (s *Server) annotatePlatformOrderAccountHealth(ctx context.Context, accounts []platformOrderAccountOption) {
	var wait sync.WaitGroup
	for index := range accounts {
		accounts[index].Available = true
		accounts[index].Status = "configured"
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			account, err := s.selectedPlatformOrderAccount(ctx, accounts[index].Key)
			if err != nil {
				accounts[index].Available = false
				accounts[index].Status = "offline"
				accounts[index].Error = "所选 OMS 账户暂不可用"
				return
			}
			checker, ok := account.(platformOrderAccessChecker)
			if !ok {
				return
			}
			if checkErr := checker.CheckAccess(ctx); checkErr != nil {
				accounts[index].Available = false
				accounts[index].Status = "offline"
				accounts[index].Error = oms.PublicAuthError(checkErr)
			} else {
				accounts[index].Status = "ready"
			}
		}(index)
	}
	wait.Wait()
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

type platformOrderAccountCredentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) updatePlatformOrderAccount(writer http.ResponseWriter, request *http.Request) {
	var payload platformOrderAccountCredentialsRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	accountKey, err := requestedPlatformOrderAccountWithBody(request, request.PathValue("accountKey"))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户参数冲突"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	updater, ok := s.platformAccounts.(platformOrderAccountUpdater)
	if !ok {
		writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "所选 OMS 账户暂不可更新"})
		return
	}
	if err := updater.UpdateAccountCredentials(ctx, accountKey, payload.Username, payload.Password); err != nil {
		if errors.Is(err, errPlatformOrderAccountNotFound) {
			writePlatformOrderAccountError(writer, err)
			return
		}
		if errors.Is(err, oms.ErrPasswordUpdateRequired) {
			writeJSON(writer, http.StatusConflict, response{
				Success: false, Error: "领星要求更新登录密码", Code: "OMS_PASSWORD_UPDATE_REQUIRED",
			})
			return
		}
		if message := oms.AuthErrorMessage(err); message != "" {
			writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: message})
			return
		}
		s.logger.Warn("update OMS platform order account", "account", accountKey, "error", err)
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "无法更新 OMS 账户"})
		return
	}
	accounts, err := s.availablePlatformOrderAccounts(ctx)
	if err != nil {
		s.logger.Warn("reload OMS platform order accounts after update", "error", err)
		writeJSON(writer, http.StatusOK, response{Success: true, Data: []platformOrderAccountOption{}})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: accounts})
}

type platformOrderAccountPasswordUpgradeRequest struct {
	Username           string `json:"username"`
	CurrentPassword    string `json:"current_password"`
	NewPassword        string `json:"new_password"`
	ConfirmNewPassword string `json:"confirm_new_password"`
}

func (s *Server) upgradePlatformOrderAccountPassword(writer http.ResponseWriter, request *http.Request) {
	var payload platformOrderAccountPasswordUpgradeRequest
	if !decodeJSON(writer, request, &payload) {
		return
	}
	if payload.NewPassword == "" || payload.NewPassword != payload.ConfirmNewPassword {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "两次输入的新密码不一致"})
		return
	}
	accountKey, err := requestedPlatformOrderAccountWithBody(request, request.PathValue("accountKey"))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户参数冲突"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	upgrader, ok := s.platformAccounts.(platformOrderAccountPasswordUpgrader)
	if !ok {
		writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "所选 OMS 账户暂不支持密码更新"})
		return
	}
	if err := upgrader.UpgradeAccountPassword(ctx, accountKey, payload.Username, payload.CurrentPassword, payload.NewPassword); err != nil {
		switch {
		case errors.Is(err, errPlatformOrderAccountNotFound):
			writePlatformOrderAccountError(writer, err)
		case errors.Is(err, oms.ErrInvalidNewPassword):
			writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "新密码不符合领星密码规则"})
		case errors.Is(err, oms.ErrPasswordUpdateNotRequired):
			writeJSON(writer, http.StatusConflict, response{
				Success: false, Error: "该账号当前不需要强制更新密码", Code: "OMS_PASSWORD_UPDATE_NOT_REQUIRED",
			})
		case errors.Is(err, oms.ErrPasswordUpdateRequired):
			writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: "领星未返回可用的密码更新会话"})
		case oms.AuthErrorMessage(err) != "":
			writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: oms.AuthErrorMessage(err)})
		default:
			s.logger.Warn("upgrade OMS platform order account password", "account", accountKey, "error", err)
			writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: "无法更新 OMS 登录密码"})
		}
		return
	}
	accounts, err := s.availablePlatformOrderAccounts(ctx)
	if err != nil {
		s.logger.Warn("reload OMS platform order accounts after password update", "error", err)
		writeJSON(writer, http.StatusOK, response{Success: true, Data: []platformOrderAccountOption{}})
		return
	}
	writeJSON(writer, http.StatusOK, response{Success: true, Data: accounts})
}

type platformOrderAccountUpdater interface {
	UpdateAccountCredentials(context.Context, string, string, string) error
}

type platformOrderAccountPasswordUpgrader interface {
	UpgradeAccountPassword(context.Context, string, string, string, string) error
}

func (p *postgresPlatformOrderAccounts) UpdateAccountCredentials(ctx context.Context, key, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return errors.New("OMS username and password are required")
	}
	probe := oms.NewClient(p.baseURL, username, password, p.timeout)
	if err := probe.CheckAccess(ctx); err != nil {
		return err
	}
	return p.saveVerifiedAccountCredentials(ctx, key, username, password, probe)
}

func (p *postgresPlatformOrderAccounts) UpgradeAccountPassword(ctx context.Context, key, username, currentPassword, newPassword string) error {
	username = strings.TrimSpace(username)
	if username == "" || currentPassword == "" || newPassword == "" {
		return errors.New("OMS username, current password, and new password are required")
	}
	if err := p.validateAccountKey(ctx, key); err != nil {
		return err
	}
	probe := oms.NewClient(p.baseURL, username, currentPassword, p.timeout)
	if err := probe.UpgradeRequiredPassword(ctx, newPassword); err != nil {
		return err
	}
	return p.saveVerifiedAccountCredentials(ctx, key, username, newPassword, probe)
}

func (p *postgresPlatformOrderAccounts) validateAccountKey(ctx context.Context, key string) error {
	accounts, err := p.selectableAccounts(ctx)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = defaultPlatformOrderAccountKey
	}
	for _, selectable := range accounts {
		if platformOrderAccountKeyMatches(selectable.option, key) {
			return nil
		}
	}
	return errPlatformOrderAccountNotFound
}

func (p *postgresPlatformOrderAccounts) saveVerifiedAccountCredentials(ctx context.Context, key, username, password string, verified platformOrderAccount) error {
	accounts, err := p.selectableAccounts(ctx)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = defaultPlatformOrderAccountKey
	}
	for _, selectable := range accounts {
		if !platformOrderAccountKeyMatches(selectable.option, key) {
			continue
		}
		if strings.EqualFold(selectable.option.Key, defaultPlatformOrderAccountKey) {
			if _, err := p.store.SetOMSAccount(ctx, defaultPlatformOrderAccountKey, username, password); err != nil {
				return err
			}
			p.replaceShared(username, password, verified)
			return nil
		}
		if len(selectable.option.WarehouseCodes) == 0 {
			return errPlatformOrderAccountUnavailable
		}
		for _, warehouseCode := range selectable.option.WarehouseCodes {
			if _, err := p.store.SetWarehouseOMSAccount(ctx, warehouseCode, username, password); err != nil {
				return err
			}
		}
		p.forgetClients()
		return nil
	}
	return errPlatformOrderAccountNotFound
}

func (p *postgresPlatformOrderAccounts) replaceShared(username, password string, client platformOrderAccount) {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.shared = client
	p.sharedUsername = username
	p.sharedPassword = password
	p.clients = nil
}

func (p *postgresPlatformOrderAccounts) forgetClients() {
	p.clientMu.Lock()
	defer p.clientMu.Unlock()
	p.clients = nil
}

func writePlatformOrderAccountError(writer http.ResponseWriter, err error) {
	if errors.Is(err, errPlatformOrderAccountNotFound) {
		writeJSON(writer, http.StatusBadRequest, response{Success: false, Error: "OMS 账户无效或已停用"})
		return
	}
	writeJSON(writer, http.StatusServiceUnavailable, response{Success: false, Error: "所选 OMS 账户暂不可用"})
}

func writePlatformOrderSourceError(writer http.ResponseWriter, err error, fallback string) {
	if message := oms.AuthErrorMessage(err); message != "" {
		writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: message})
		return
	}
	writeJSON(writer, http.StatusBadGateway, response{Success: false, Error: fallback})
}
