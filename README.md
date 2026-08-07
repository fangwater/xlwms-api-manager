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
- 独立 Go SHEIN 订单、在线物流下单、面单和物流轨迹工作台
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
GET    /v1/platform-orders/pending
POST   /v1/platform-orders/routing-preview
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

平台订单自动分仓只使用履约核查中由 Temu 购面单账本同步的仓库；没有完整、唯一的购面单仓库记录时禁止自动审核，不使用 OMS 平台仓字段兜底。

库存警告按启用仓的正品可用库存计算。未单独配置的“仓库 + SKU”使用默认告警线 `100`，可用库存低于或等于告警线时进入告警列表；单项配置通过仓库码和仓库 SKU 精确覆盖。

出库操作统一使用 `POST /v1/outbound/{operation}`，`operation` 可选：`parcel-create`、`parcel-list`、`parcel-detail`、`parcel-cancel`、`cancel-status`、`bulk-product-create`、`bulk-list`、`bulk-detail`、`bulk-cancel`、`tracking-label-update`、`bulk-box-create`、`message-detail`、`message-reply`。请求体为 `{"warehouse":"WH_CODE","data":...}`。出库列表和详情实时查询领星，不在本地复制收件信息。

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

- 任一包裹出现 `Last Mile Carrier Pick up failed` 时，立即归类为“揽收异常订单”。
- 自领星出库时间起达到 24 小时，仍有包裹未出现 `Last Mile Carrier Picked up` 时，归类为“揽收异常订单”；`Last-Mile Manifest` 包含在该规则内。
- 订单的所有包裹均出现 `Last Mile Carrier Picked up` 后，才归类为“已揽收”。

`GET /v1/fulfillment-audits/archived` 支持 `shop`、`warehouse`、
`tracking_category` 和 `q` 组合筛选。追踪分类为 `awaiting_pickup`、`picked_up`、
`pickup_exception` 或 `tracking_error`。揽收异常与普通待追踪订单使用独立的持久化
watermark 轮转，互不挤占批次；Temu 查询失败会保存本次查询时间和错误，只有本地结果
未保存成功时才停止推进 watermark。每个队列的单次追踪量和共享并发数分别由
`XLWMS_FULFILLMENT_TRACKING_LIMIT`、`XLWMS_FULFILLMENT_TRACKING_CONCURRENCY` 控制。

## SHEIN Go 工作台

生产入口为 `https://pangutech.online/shein/`。该服务是独立 Go 进程，复用现有
SHEIN PostgreSQL 中的店铺授权凭证，不调用 Python 服务转发开放平台请求。

```text
POST /shein/api/order/list
POST /shein/api/order/detail
POST /shein/api/order/export-address
POST /shein/api/shipping/warehouses
POST /shein/api/shipping/channels
POST /shein/api/shipping/place
POST /shein/api/shipping/check
POST /shein/api/shipping/label
GET  /shein/api/shipping/track
```

在线下单、地址状态流转和打印面单要求 `X-Confirm-Shein-Action` 与
`Idempotency-Key`。幂等记录只保存请求哈希、状态和必要响应，不保存客户地址。
Go 服务验证现有 `shein_pnl_session` 登录 Cookie，因此已登录 SHEIN 管理台的用户
无需再次登录。详细接口说明见 `docs/shein-go.md`。


- 真实凭据只保存在模式为 `600` 的本地 `.env` 中。
- 仓库凭据使用 Fernet 加密后写入 `xlwms_warehouses`。
- Fernet 主密钥仅保存在模式为 `600` 的 `.warehouse_credentials_key`。
- 仓库列表只返回 App Key 提示，不返回可解密凭据。
## 安全约束
- 同步任务只读取启用仓库，服务仅允许监听回环地址。
- 不要记录、提交或公开原始 API 响应中的客户、财务和物流数据。
