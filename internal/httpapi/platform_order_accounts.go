package httpapi

import (
	"context"
	"time"

	"xlwms-api-manager/internal/oms"
	"xlwms-api-manager/internal/store"
)

type platformOrderAccountSource interface {
	OperatorForWarehouse(context.Context, string) (platformOrderOperator, error)
}

type fixedPlatformOrderAccounts struct {
	operator platformOrderOperator
}

func (f fixedPlatformOrderAccounts) OperatorForWarehouse(context.Context, string) (platformOrderOperator, error) {
	return f.operator, nil
}

type postgresPlatformOrderAccounts struct {
	store   *store.Postgres
	baseURL string
	timeout time.Duration
}

func (p *postgresPlatformOrderAccounts) OperatorForWarehouse(ctx context.Context, warehouseCode string) (platformOrderOperator, error) {
	account, err := p.store.WarehouseOMSAccount(ctx, warehouseCode, true)
	if err != nil {
		return nil, err
	}
	return oms.NewClient(p.baseURL, account.Username, account.Password, p.timeout), nil
}
