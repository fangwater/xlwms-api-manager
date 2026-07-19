# XLWMS API Manager

Local Python client for Lingxing WMS OpenAPI workflows.

The project currently provides secure environment-based configuration. The
first planned API workflow is the parcel outbound-order page endpoint:
`POST /v1/outboundOrder/pageList`.

## Setup

```bash
. /home/ubuntu/.venv/bin/activate
cd /home/ubuntu/xlwms-api-manager
pip install -e .
```

The shared environment at `/home/ubuntu/.venv` is also used by the Temu API
Manager. Do not create a project-local virtual environment.

Enter credentials issued by Lingxing WMS in the local `.env` file:

```dotenv
XLWMS_API_BASE_URL=https://api.xlwms.com/openapi
XLWMS_APP_KEY=your_app_key
XLWMS_APP_SECRET=your_app_secret
```

The `.env` file is ignored by Git and restricted to the current user. Do not
commit or print its contents.


## Query Cost Funds Flow

```bash
xlwms-manager funds-flow --page 1 --page-size 100
```

Optional filters can be supplied as JSON. Page fields are controlled by the
dedicated options:

```bash
xlwms-manager funds-flow \
  --params-json '{"moduleTypeList":[2],"currencyCodeList":["USD"]}' \
  --page 1 \
  --page-size 100 \
  --output exports/funds-flow.json
```

## Query Cost Detail

Use an OMS order number from the funds-flow response:

```bash
xlwms-manager cost-detail \
  --query-order-no "OMS_ORDER_NO" \
  --query-order-type 1 \
  --module-type 2 \
  --output exports/cost-detail.json
```

`query-order-type` is `1` for an OMS order number or `2` for a cost order
number. The response contains the cost header and a `costItemList`; each item
contains `billItemName`, `billItemTotal`, and `chargeTime`.

Raw cost exports contain sensitive financial data and are written with file
mode `600`.

## Initialize PostgreSQL

The application uses the dedicated `xlwms` database owned by `xlwms_app`.
Create or update its tables with:

```bash
xlwms-manager db-init
```

The schema separates funds-flow rows, cost headers, and cost items into
`xlwms_funds_flows`, `xlwms_cost_details`, and `xlwms_cost_items`.

## Manage Warehouses

Warehouse credentials are encrypted in PostgreSQL with a local Fernet key. Add
or update a warehouse from credential environment variables:

```bash
xlwms-manager warehouse-add --wh-code HYTX30 --name "HYTX30"
xlwms-manager warehouse-list
xlwms-manager warehouse-disable HYTX30
xlwms-manager warehouse-enable HYTX30
```

API commands can select a registered warehouse with `--warehouse HYTX30`.
Disabled warehouses cannot be used, and batch synchronization must load only
active credentials through the warehouse registry.

## Synchronize Funds Flow

Regular synchronization processes only active warehouses:

```bash
xlwms-manager sync-funds-flow --all-active
```

A disabled warehouse can be synchronized only with an explicit one-time
override:

```bash
xlwms-manager sync-funds-flow --warehouse PA30 --include-disabled
```

## Backfill Cost Details

Cost details resume from funds-flow rows not yet marked successful. Disabled
warehouses require an explicit override:

```bash
xlwms-manager sync-cost-details \
  --warehouse PA30 \
  --include-disabled \
  --workers 4 \
  --requests-per-second 8
```
