# XLWMS API Manager

领星 WMS 的本地仓库运营看板与管理系统。Go 服务是当前主运行时，提供凭据管理、数据同步、库存查询和费用管理 API；React + TypeScript 前端提供面向日常运营的工作台。

原 Python CLI 暂保留在 `src/xlwms_api_manager`，仅用于迁移回溯，不再是主应用入口。

## 已实现功能

- 多仓库注册、启停和凭据加密存储
- 综合库存、产品库龄、产品库存流水
- 箱库存、箱库龄、箱分段库龄、箱库存流水
- 资金流水同步、费用明细补全和失败重试
- 库存概览、库龄结构、仓库库存分布和同步记录
- 按仓库、SKU、产品、箱型、条码和关联单号查询
- Temu 发货前实时 SKU 库存查询、东西区域安全库存判断和 DPS 优先选仓
- Temu 已出库订单追踪、24 小时揽收超时识别及店铺/仓库维度筛选
- 桌面与移动端自适应管理界面

已接入的官方库存端点：

```text
POST /v1/integratedInventory/pageOpen
POST /v1/integratedInventory/pageStockAge
POST /v1/integratedInventory/pageStockFlow
POST /v1/boxStock/page
POST /v1/boxStock/pageStockAge
POST /v1/boxStock/pageSegmentStockAge
POST /v1/boxStock/pageStockFlow
```
已接入的官方出库端点：

```text
POST /v1/outboundOrder/create
POST /v1/outboundOrder/pageList
POST /v1/outboundOrder/detail
POST /v1/outboundOrder/cancel
POST /v1/outboundOrder/selectBizStatus
POST /v1/outboundOrder/big/create
POST /v1/outboundOrder/big/pageList
POST /v1/outboundOrder/big/detail
POST /v1/outboundOrder/big/cancel
POST /v1/outboundOrder/updateTrackNoAndLabel
POST /v1/outboundOrder/big/bulkBox/create
POST /v1/outboundOrder/big/messageBoard/detail
POST /v1/outboundOrder/big/messageBoard/reply
```


默认 OpenAPI 地址为 `https://api.xlwms.com/openapi`，可通过 `XLWMS_API_BASE_URL` 覆盖。

## 技术结构

```text
cmd/server              Go HTTP 服务入口
internal/xlwms          官方 API 客户端、签名和参数校验
internal/store          PostgreSQL 仓储、库存快照和费用数据
internal/syncer         后台分页同步、限流与重试
internal/httpapi        管理端 HTTP API
migrations              嵌入式数据库迁移
frontend                 React 19 + TypeScript + Vite + ECharts
```

履约快递评价使用独立 Python 服务 `/home/ubuntu/xlwms-fulfillment-evaluator`，
前端仍由本项目承载。页面路由为 `/delivery-evaluation`，开发环境通过
`/warehouse-console/evaluation-api/` 代理到 `127.0.0.1:18087`。

## 本地运行

需要 Go 1.26、Node.js 26、npm 12 和 PostgreSQL。

```bash
cp .env.example .env
chmod 600 .env
npm --prefix frontend ci
```

在 `.env` 中配置 `DATABASE_URL`。全局 `XLWMS_APP_KEY` 和 `XLWMS_APP_SECRET` 只用于本地初始配置；日常同步从加密仓库注册表读取每个启用仓库的凭据。平台订单栏目仅由后端读取 `XLWMS_OMS_USERNAME` 和 `XLWMS_OMS_PASSWORD`；前端不接触凭据或 OMS Token。物流匹配根据订单的平台仓 ID 和 Temu Go 中已有的仓库映射自动确定实际发货仓，无法精确映射时禁止审核。

分别启动后端和前端：

```bash
make dev-api
make dev-web
```

打开 `http://localhost:5174/warehouse-console/`。后端默认只监听 `127.0.0.1:18083`，前端开发服务器通过 Vite 代理访问它。

## 构建与验证

```bash
make test
make build
```

生产地址为 `https://pangutech.online/warehouse-console/`，当前不启用 Nginx Basic Auth。

生产前端输出到 `frontend/dist`，Go 可执行文件输出到 `bin/xlwms-server`。

## 管理 API

主要本地端点：

```text
GET    /healthz
GET    /v1/dashboard/summary
GET    /v1/platform-orders/accounts
PATCH  /v1/platform-orders/accounts/{accountKey}
GET    /v1/platform-orders/pending
GET    /v1/platform-orders/{platformOrderNo}
GET    /v1/temu/platform-orders/{platformOrderNo}
POST   /v1/platform-orders/routing-preview
POST   /v1/platform-orders/warehouse-assignments
POST   /v1/platform-orders/assign-and-approve
GET    /v1/warehouses
POST   /v1/warehouses
PATCH  /v1/warehouses/{code}/status
GET    /v1/inventory
GET    /v1/inventory/sku-levels
GET    /v1/inventory-alerts
PATCH  /v1/inventory-alerts/default
PATCH  /v1/inventory-alerts/config
POST   /v1/inventory-alerts/config/reset
POST   /v1/inventory/query/{kind}
POST   /v1/temu/warehouse-availability/query
GET    /v1/fulfillment-audits
GET    /v1/fulfillment-audits/archived
GET    /v1/fulfillment-audits/export-manual
POST   /v1/fulfillment-audits/sync
POST   /v1/sync/inventory
GET    /v1/funds-flows
GET    /v1/cost-details
POST   /v1/sync/funds-flow
POST   /v1/sync/cost-details
POST   /v1/outbound/{operation}
GET    /v1/sync/runs
```
`GET /v1/platform-orders/pending` 支持 `q` 参数按平台单号精确查询；仅返回当前仍处于待处理状态的订单。

`GET /v1/platform-orders/{platformOrderNo}` 实时查询所选 OMS 账户的“全部订单”，
不附加待处理状态筛选，也不在本地同步或保存账户订单。响应中的 `found` 表示该账户
是否存在这个平台单号，`records` 保留 OMS 返回的匹配订单及 `orderTime`、
`createTime`、`status` 等字段。

账户选择方式与 Temu 服务的 `X-Temu-Shop` 模式一致，优先使用
`X-OMS-Account`；也可以使用 `account` 查询参数。两者同时提供时必须一致。
缺省账户为 `arp`，可选键通过 `GET /v1/platform-orders/accounts` 获取。
该接口会探测每个账户的领星登录，并返回 `available`、`status` 和 `error`；
已配置但无法换票的账户仍会出现在列表里，但 `available=false`：

```bash
curl -sS \
  -H 'X-OMS-Account: warehouse:DPSCA004' \
  'http://127.0.0.1:18083/v1/platform-orders/PO-DEMO-1001'
```

```json
{
  "success": true,
  "data": {
    "account": "warehouse:DPSCA004",
    "platform_order_no": "PO-DEMO-1001",
    "found": true,
    "records": [
      {
        "orderNo": "OMS-DEMO-1",
        "platformOrderNo": "PO-DEMO-1001",
        "orderTime": "2026-08-10 09:00:00",
        "createTime": "2026-08-10 09:05:00",
        "status": 2
      }
    ],
    "queried_at": "2026-08-11T00:00:00Z"
  }
}
```

Temu Go 服务使用 `GET /v1/temu/platform-orders/{platformOrderNo}` 查询领星确认状态。
该服务间端点必须显式传入 `X-OMS-Account` 或 `account`，不会默认查询 ARP，避免在
ARP、DPS 数据隔离时误查账户。端点实时查询 OMS“全部订单”，不读取或写入本地订单表，
并只返回后续核验所需的最小字段：

服务间调用使用稳定的账户归属键 `dps` 或 `arp`。`warehouse:<实际仓库代码>` 仍可作为
人工查询别名，例如 `warehouse:DPSNY002` 和 `warehouse:DPSCA004` 会选择同一个 DPS
凭据组，`warehouse:HYTX30` 会选择 ARP 凭据组。

“全部订单”精确查询在每个 OMS 凭据组内最多并发 2 个请求，请求启动间隔至少 500ms。
等待限频槽位时遵守调用方上下文超时，超时后由上层账本按既有节奏重试。

```bash
curl -sS \
  -H 'X-OMS-Account: dps' \
  'http://127.0.0.1:18083/v1/temu/platform-orders/PO-DEMO-1001'
```

```json
{
  "success": true,
  "data": {
    "account": "dps",
    "platform_order_no": "PO-DEMO-1001",
    "found": true,
    "match_count": 1,
    "orders": [{
      "oms_order_no": "OMS-DEMO-1",
      "platform_order_no": "PO-DEMO-1001",
      "status": 2,
      "status_key": "processing",
      "status_text": "处理中",
      "send_warehouse_code": "DPSNY002",
      "audit_time": "2026-08-11 14:21:30"
    }],
    "queried_at": "2026-08-11T06:22:00Z"
  }
}
```

`status` 保留 OMS 原始数字。根据当前 OMS 官方 Web 客户端，`0` 至 `6` 分别归一化为
`pending`、`awaiting_platform_label`、`processing`、`shipped`、`canceled`、
`exception`、`awaiting_invoice`；其他数字返回 `status_key: "unknown"`。

平台订单自动分仓只使用履约核查中由 Temu 购面单账本同步的仓库；没有完整、唯一的购面单仓库记录时禁止自动审核，不使用 OMS 平台仓字段兜底。

### 平台订单仓库分配 API

待处理页面的“分配仓库和物流”操作通过以下接口开放给其他程序调用：

```text
POST /v1/platform-orders/warehouse-assignments
```

该操作会实时读取待处理订单及可靠的 Temu 购面单仓库记录，按实际仓库分组调用 OMS
`batchAllotWarehouse`，使用“上传物流面单”渠道分配仓库并立即审核。它不是仅保存仓库
选择的草稿操作。

调用方传平台订单号，不传 OMS 内部订单号。单次最多 50 单：

```bash
curl -sS \
  -X POST \
  -H 'Content-Type: application/json' \
  -H 'X-OMS-Account: dps' \
  'http://127.0.0.1:18083/v1/platform-orders/warehouse-assignments' \
  --data '{
    "platform_order_nos": ["PO-DEMO-1001", "PO-DEMO-1002"],
    "logistics_carrier": "_AUTO_MATCH_",
    "confirmation": "CONFIRM_AND_APPROVE"
  }'
```

`logistics_carrier` 只能为 `_AUTO_MATCH_` 或 `other`。OMS 账户可以通过
`X-OMS-Account`、`account` 查询参数或请求体的 `account` 选择；多个位置同时提供时
必须一致。可选账户键通过 `GET /v1/platform-orders/accounts` 获取。

成功响应会返回每个订单最终分配的仓库：

```json
{
  "success": true,
  "data": {
    "account": "dps",
    "total": 2,
    "success": 2,
    "failed": 0,
    "failures": [],
    "routes": [
      {
        "platform_order_no": "PO-DEMO-1001",
        "platform_warehouse_id": "TEMU-WH-1",
        "platform_warehouse_name": "Temu East",
        "warehouse_code": "DPSNY002",
        "warehouse_name": "DPS达派思-纽约"
      }
    ],
    "warehouse_codes": ["DPSNY002"],
    "channel_code": "Upload_Shipping_Label",
    "logistics_carrier": "_AUTO_MATCH_",
    "completed_at": "2026-08-13T06:00:00Z"
  }
}
```

调用方不能传 `warehouse_code` 覆盖仓库。没有唯一购面单仓库、订单已不在待处理状态、
所选 OMS 账户无权使用目标仓库或上传面单渠道未启用时，接口不会审核该订单并返回冲突。
成功后重复提交同一订单时，该订单已不是待处理状态，也会被拒绝。

`POST /v1/platform-orders/assign-and-approve` 作为原页面接口继续兼容；新调用应使用
`/warehouse-assignments`。

库存警告按启用仓的正品可用库存计算。未单独配置的“仓库 + SKU”使用默认告警线 `100`，可用库存低于或等于告警线时进入告警列表；单项配置通过仓库码和仓库 SKU 精确覆盖。

出库操作统一使用 `POST /v1/outbound/{operation}`，`operation` 可选：`parcel-create`、`parcel-list`、`parcel-detail`、`parcel-cancel`、`cancel-status`、`bulk-product-create`、`bulk-list`、`bulk-detail`、`bulk-cancel`、`tracking-label-update`、`bulk-box-create`、`message-detail`、`message-reply`。请求体为 `{"warehouse":"WH_CODE","data":...}`。出库列表和详情实时查询领星，不在本地复制收件信息。

### 小包裹建单

领星小包裹建单的上游端点为 `POST /v1/outboundOrder/create`，Go Server 封装为
`POST /v1/outbound/parcel-create`。先通过 `GET /v1/warehouses?active_only=true` 获取可选仓库，
请求体的 `warehouse` 会选择该仓在 `xlwms_warehouses` 中加密保存的 API 凭据和
`api_base_url`。禁用仓库会被拒绝，且每条订单的 `whCode` 必须与所选仓库一致。

```json
{
  "warehouse": "WH_CODE",
  "data": [
    {
      "whCode": "WH_CODE",
      "thirdOrderNo": "ORDER-10001",
      "subOrderType": 1,
      "logisticsChannel": "CHANNEL_CODE",
      "receiver": "Test Receiver",
      "countryRegionCode": "US",
      "provinceCode": "CA",
      "provinceName": "California",
      "cityName": "Los Angeles",
      "postCode": "90001",
      "addressOne": "Test address",
      "productList": [
        {
          "sku": "SKU-1",
          "quantity": 1
        }
      ]
    }
  ]
}
```

单次最多传 100 个小包订单。Go Server 仅使用选中仓库的凭据签名并原样转发
`data`，调用方不得传入 App Key 或 Secret。领星会校验具体业务字段；可选字段可以
继续放入每个订单对象中。

`kind` 使用 `integrated`、`stock_age`、`stock_flow`、`box_stock`、`box_stock_age`、`box_segment_age` 或 `box_stock_flow`。

### Temu 发货仓库查询

`POST /v1/temu/warehouse-availability/query` 实时查询领星 OMS 的正品产品可用库存，不读取本地库存快照。请求可以传单个 SKU 或最多 100 个 SKU：

```json
{"sku":"SKU-1"}
```

```json
{"skus":["SKU-1","SKU-2"]}
```

仓库业务标识与领星仓库代码：

- 美东：`DPS002 -> DPSNY002`，`ARP_EAST -> HYTX30`
- 美西：`DPS004 -> DPSCA004`，`ARP_WEST -> ARPCA01`

返回结果包含各仓正品产品可用库存、是否可选、推荐仓、区域库存合计、是否转人工、稳定原因码和中文理由。规则为：

- 单仓可用库存小于等于 0 时不可选择。
- 美东或美西区域内两仓可用库存合计小于等于 50 时，该区域转人工。

## Temu 出库物流跟踪

XLWMS 不保存 Temu 凭据，也不直接签名 Temu OpenAPI 请求。追踪查询统一调用独立的
`/home/ubuntu/temu-api-manager` Go 服务；默认地址为
`http://127.0.0.1:18082/temu`，可通过 `TEMU_GO_BASE_URL` 覆盖。XLWMS 按订单店铺发送
`X-Temu-Shop`，通过 `GET /api/orders/{parentOrderSN}/tracking?language=en` 获取包裹轨迹。

后台履约核查任务会追踪领星已出库的 Temu 订单，并仅保存运营所需的标准化状态：

- 任一包裹出现 `Last Mile Carrier Pick up failed` 时，出库未满 12 小时仍按“待揽收”处理且不计入异常统计；
  满 12 小时后仍未恢复正常流程，才归类为“揽收异常订单”。
- 自领星出库时间起达到 24 小时，仍有包裹未出现 `Last Mile Carrier Picked up` 时，归类为“揽收异常订单”；`Last-Mile Manifest` 包含在该规则内。
- 订单的所有包裹均出现 `Last Mile Carrier Picked up` 后，才归类为“已揽收”；若换单号后的轨迹缺少揽收节点，但包裹已显示 `In transit` 或 `Delivered`，同样视为已经完成揽收。

`GET /v1/fulfillment-audits/archived` 支持 `shop`、`warehouse`、
`tracking_category` 和 `q` 组合筛选。追踪分类为 `awaiting_pickup`、`picked_up`、
`pickup_exception` 或 `tracking_error`。揽收异常与普通待追踪订单使用独立的持久化
watermark 轮转，互不挤占批次；Temu 查询失败会保存本次查询时间和错误，只有本地结果
未保存成功时才停止推进 watermark。每个队列的单次追踪量和共享并发数分别由
`XLWMS_FULFILLMENT_TRACKING_LIMIT`、`XLWMS_FULFILLMENT_TRACKING_CONCURRENCY` 控制。

- 真实凭据只保存在模式为 `600` 的本地 `.env` 中。
- 仓库凭据使用 Fernet 加密后写入 `xlwms_warehouses`。
- Fernet 主密钥仅保存在模式为 `600` 的 `.warehouse_credentials_key`。
- 仓库列表只返回 App Key 提示，不返回可解密凭据。
## 安全约束
- 同步任务只读取启用仓库，服务仅允许监听回环地址。
- 不要记录、提交或公开原始 API 响应中的客户、财务和物流数据。
