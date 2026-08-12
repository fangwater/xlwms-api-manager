import { expect, test, type Page } from "@playwright/test";

const warehouses = [
  { wh_code: "EAST-01", name: "华东一号仓", api_base_url: "https://api.xlwms.com/openapi", app_key_hint: "demo...key", oms_account_configured: true, oms_account_hint: "op***01", active: true, updated_at: "2026-08-01T08:00:00Z" },
  { wh_code: "WEST-02", name: "西部分拨仓", api_base_url: "https://api.xlwms.com/openapi", app_key_hint: "demo...key", oms_account_configured: false, oms_account_hint: "", active: true, updated_at: "2026-08-01T08:00:00Z" }
];

async function mockAPI(page: Page, options: { indexedParcelMatch?: boolean } = {}) {
  await page.route("**/warehouse-console/healthz", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ success: true, data: { status: "ok" } }) }));
  await page.route("**/warehouse-console/api/**", (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    if (path.endsWith("/fulfillment-audits/export-manual")) {
      const split = url.searchParams.get("split_by_warehouse") === "true";
      return route.fulfill({ status: 200, contentType: split ? "application/zip" : "text/csv; charset=utf-8", headers: { "Content-Disposition": `attachment; filename=manual-fulfillment-orders-demo.${split ? "zip" : "csv"}` }, body: split ? "PK" : "店铺,平台PO单号\nPANDA HOMES,PO-DEMO-1001\n" });
    }
    let data: unknown = {};
    if (path.endsWith("/warehouses")) data = warehouses;
    else if (path.endsWith("/dashboard/summary")) data = {
      operations: {
        active_warehouses: 2, total_warehouses: 2, funds_flows: 1284, cost_details: 1209,
        pending_details: 63, failed_details: 12, cost_by_currency: { USD: 48200 },
        trend: [
          { date: "2026-07-26", amount: 68 }, { date: "2026-07-27", amount: 92 },
          { date: "2026-07-28", amount: 76 }, { date: "2026-07-29", amount: 108 },
          { date: "2026-07-30", amount: 124 }, { date: "2026-07-31", amount: 116 },
          { date: "2026-08-01", amount: 139 }
        ]
      },
      inventory: {
        total_amount: 186420, available_amount: 153280, lock_amount: 12640, transport_amount: 20500,
        sku_count: 2386, box_type_count: 147, stale_amount: 6840,
        stock_by_warehouse: { "华东一号仓": 112800, "西部分拨仓": 73620 },
        age_buckets: { "0_30": 98200, "31_60": 43800, "61_90": 21200, "91_180": 16380, "181_plus": 6840 }
      }
    };
    else if (path.endsWith("/inventory/sku-levels")) data = {
      records: [{
        sku: "DEMO-SKU-01", product_name: "演示产品", stock_type: 0, total_amount: 42,
        available_amount: 36, lock_amount: 4, transport_amount: 2, warehouse_count: 2,
        warehouses: {
          "EAST-01": { total_amount: 30, available_amount: 26, lock_amount: 3, transport_amount: 1 },
          "WEST-02": { total_amount: 12, available_amount: 10, lock_amount: 1, transport_amount: 1 }
        },
        last_seen_at: "2026-08-01T08:00:00Z"
      }],
      total: 1, page: 1, page_size: 30, pages: 1,
      summary: { sku_count: 1, record_count: 1, total_amount: 42, available_amount: 36, lock_amount: 4, transport_amount: 2 }
    };
    else if (path.endsWith("/inventory-alerts/default")) data = { threshold: 100 };
    else if (path.endsWith("/inventory-alerts/config/reset")) data = { deleted: true };
    else if (path.endsWith("/inventory-alerts/config")) data = {
      wh_code: route.request().postDataJSON()?.wh_code || "EAST-01",
      warehouse_sku: route.request().postDataJSON()?.warehouse_sku || "DEMO-SKU-01",
      threshold: route.request().postDataJSON()?.threshold || 100,
      updated_at: "2026-08-06T08:00:00Z"
    };
    else if (path.endsWith("/inventory-alerts")) {
      const records = [{
        wh_code: "EAST-01", wh_name: "华东一号仓", warehouse_sku: "DEMO-SKU-01", product_name: "演示产品",
        total_amount: 92, available_amount: 80, lock_amount: 8, transport_amount: 4,
        threshold: 100, customized: false, alert: true,
        inventory_at: "2026-08-06T07:40:00Z", config_updated_at: "2026-08-06T07:00:00Z"
      }, {
        wh_code: "WEST-02", wh_name: "西部分拨仓", warehouse_sku: "DEMO-SKU-02", product_name: "正常水位产品",
        total_amount: 168, available_amount: 140, lock_amount: 18, transport_amount: 10,
        threshold: 120, customized: true, alert: false,
        inventory_at: "2026-08-06T07:42:00Z", config_updated_at: "2026-08-06T07:10:00Z"
      }];
      const visible = url.searchParams.get("status") === "all" ? records : records.filter((item) => item.alert);
      data = {
        records: visible, total: visible.length, page: 1, page_size: 50, pages: 1, default_threshold: 100,
        summary: { alert_count: 1, out_of_stock_count: 0, warehouse_count: 2, sku_count: 2 }
      };
    }
    else if (path.endsWith("/inventory")) data = { records: [], total: 0, page: 1, page_size: 30, pages: 1 };
    else if (path.endsWith("/fulfillment-audits/archived")) data = {
      records: [{
        id: 2, platform: "temu", shop_code: "panda-buy", shop_name: "PANDA BUY",
        platform_order_no: "PO-ARCHIVED-1002", platform_status: "pending_pickup", platform_status_code: 4,
        wh_code: "HYTX30", warehouse_key: "arp-east", tracking_number: "TRACK-ARCHIVED",
        oms_status: "outbound", oms_status_code: 3, outbound_order_no: "OB-ARCHIVED-1002",
        oms_tracking_number: "TRACK-ARCHIVED", last_mile_tracking_number: "LAST-MILE-1002",
        tracking_status: "Last-Mile Manifest", tracking_status_text: "Waiting for carrier pickup",
        tracking_category: "pickup_exception", pickup_exception_reason: "pickup_overdue",
        tracking_package_count: 1, picked_up_package_count: 0, exception_category: "archived", active: false,
        oms_outbound_at: "2026-08-05T06:40:00Z", tracking_updated_at: "2026-08-05T07:40:00Z",
        tracking_checked_at: "2026-08-06T08:00:00Z", updated_at: "2026-08-06T08:00:00Z"
      }],
      total: 2007, page: 1, page_size: 30, pages: 67, last_query_at: "2026-08-06T07:00:00Z",
      last_tracking_at: "2026-08-06T08:00:00Z",
      summary: { total: 2007, awaiting_pickup: 100, picked_up: 1820, pickup_exception: 82, tracking_error: 5, last_tracking_at: "2026-08-06T08:00:00Z" },
      shops: [{ code: "panda-buy", name: "PANDA BUY" }]
    };
    else if (path.endsWith("/fulfillment-audits")) data = {
      records: [{
        id: 1, platform: "temu", shop_code: "panda-homes", shop_name: "PANDA HOMES",
        platform_order_no: "PO-DEMO-1001", platform_status: "pending_pickup", platform_status_code: 4,
        wh_code: "HYTX30", warehouse_key: "arp-east", tracking_number: "TRACK-DEMO",
        oms_status: "exception", oms_status_code: 4, outbound_order_no: "OB-DEMO-1001",
        oms_tracking_number: "TRACK-DEMO", exception_category: "manual_required", active: true,
        first_seen_at: "2026-08-01T08:00:00Z", last_seen_at: "2026-08-01T08:00:00Z",
        last_checked_at: "2026-08-01T08:05:00Z", updated_at: "2026-08-01T08:05:00Z"
      }],
      total: 1, page: 1, page_size: 30, pages: 1,
      summary: { total: 400, pending_query: 0, manual_required: 20, warehouse_overdue: 0, sync_error: 0, monitoring: 380, last_query_at: "2026-08-05T07:00:00Z" },
      shops: [{ code: "panda-homes", name: "PANDA HOMES" }, { code: "panda-buy", name: "PANDA BUY" }]
    };
    else if (path.endsWith("/platform-orders/routing-preview")) data = {
      ready: true,
      routes: [{
        platform_order_no: "PO-DEMO-2201", platform_warehouse_id: "WH-DEMO",
        platform_warehouse_name: "平台美东仓", warehouse_code: "HYTX30", warehouse_name: "美东演示仓"
      }],
      unresolved: [],
      channel_code: "Upload_Shipping_Label", channel_name: "上传物流面单",
      carriers: [{ value: "_AUTO_MATCH_", label: "自动匹配" }, { value: "other", label: "Other" }],
      queried_at: "2026-08-06T05:20:00Z"
    };
    else if (path.endsWith("/platform-orders/assign-and-approve")) data = {
      total: 1, success: 1, failed: 0, failures: [], warehouse_code: "HYTX30", warehouse_codes: ["HYTX30"],
      channel_code: "Upload_Shipping_Label", logistics_carrier: route.request().postDataJSON()?.logistics_carrier || "_AUTO_MATCH_",
      completed_at: "2026-08-06T05:25:00Z"
    };
    else if (path.endsWith("/platform-orders/accounts")) data = [
      { key: "arp", label: "ARP 账户", warehouse_codes: ["ARPCA01", "ARPGA", "HYTX30"] },
      { key: "warehouse:DPSCA004", label: "DPS 账户", warehouse_codes: ["DPSCA004", "DPSNY002"] }
    ];
    else if (path.endsWith("/platform-orders/pending")) {
      const searched = Boolean(url.searchParams.get("q"));
      const dps = url.searchParams.get("account") === "warehouse:DPSCA004";
      data = {
        records: [{
        orderNo: "OMS-DEMO-2201", platformOrderNo: "PO-DEMO-2201", platformCode: "temu",
        platformChannelName: "Temu", platformSkuList: [{ sku: "PLATFORM-SKU-1", qty: 2, productName: "平台演示产品" }],
        skuList: [{ sku: "WAREHOUSE-SKU-1", qty: 2, productName: "仓库演示产品" }],
        platformWarehouseDetails: [{ platformSku: "PLATFORM-SKU-1", warehouseId: "WH-DEMO", warehouseName: "美东演示仓", qty: 2 }],
        storeCode: "demo-store", storeName: "PANDA HOMES", site: "US", siteNameCn: "美国站",
        sendWhCode: "HYTX30", sendWhName: "美东演示仓", receiptCountryCode: "US", receiptCountryName: "美国",
        logisticsCarrier: "UPS", logisticsCarrierName: "UPS", logisticsChannelCode: "UPS-GROUND", logisticsChannelName: "UPS Ground",
        trackNo: "TRACK-DEMO-2201", orderTime: "2026-08-06T01:20:00Z", payTime: "2026-08-06T01:21:00Z",
        createTime: "2026-08-06T01:22:00Z", requestDeliveryTime: "2026-08-12T23:59:59Z",
        status: 0, subStatus: 0, markShipmentStatus: 5, directMailOrder: false,
        remark: "", exceptionCause: "", auditCause: "", requestDeliveryTimeFailReason: "", markShipmentFailReason: "", platformSplitReason: ""
        }],
        total: searched ? 1 : (dps ? 45944 : 2222), page: searched ? 1 : Number(url.searchParams.get("page") || 1), page_size: 30, pages: searched ? 1 : (dps ? 1532 : 75), queried_at: "2026-08-06T05:20:00Z"
      };
    }
    else if (path.endsWith("/outbound-orders")) {
      const records = options.indexedParcelMatch === false ? [] : [{ whCode: "EAST-01", outboundOrderNo: "OB-DEMO-1001", thirdOrderNo: "EXT-DEMO-88", platformOrderNo: "PO-DEMO-1001", status: 2, logisticsTrackNo: "TRACK-DEMO", orderCreateTime: "2026-08-01T01:30:00Z" }];
      data = { records, total: records.length, page: 1, page_size: 30, pages: 1, last_query_at: "2026-08-05T07:00:00Z" };
    }
    else if (path.endsWith("/funds-flows")) data = { records: [{ id: 1, wh_code: "EAST-01", order_no: "OB-DEMO-1001", platform_order_no: "PO-DEMO-1001", cost_total: 12.5, currency_code: "USD", cost_time: "2026-08-03T08:00:00Z", detail_sync_status: "success", detail_attempts: 1 }], total: 1, page: 1, page_size: 30, pages: 1 };
    else if (path.endsWith("/cost-details")) data = { records: [{ wh_code: "EAST-01", cost_no: "COST-DEMO-1", query_order_no: "OB-DEMO-1001", platform_order_no: "PO-DEMO-1001", cost_total: 12.5, currency_code: "USD", create_time: "2026-08-03T08:00:00Z", item_count: 2 }], total: 1, page: 1, page_size: 30, pages: 1 };
    else if (path.includes("/outbound/parcel-list")) {
      const requestData = route.request().postDataJSON()?.data || {};
      const records = [{ whCode: "EAST-01", outboundOrderNo: "OB-DEMO-1001", thirdOrderNo: "EXT-DEMO-88", platformOrderNo: "PO-DEMO-1001", status: 2, logisticsChannel: "DEMO-CHANNEL", logisticsTrackNo: "TRACK-DEMO", orderCreateTime: "2026-08-01 09:30:00" }];
      data = { code: 200, msg: "", data: { records, total: records.length, page: requestData.page || 1, pageSize: requestData.pageSize || 30, pages: 1 } };
    }
    else if (path.endsWith("/sync/runs")) data = [];
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ success: true, data }) });
  });
}

test("desktop dashboard renders data and charts", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await mockAPI(page);
  await page.goto("./");
  await expect(page.getByRole("heading", { name: "运营总览" })).toBeVisible();
  await expect(page.getByText("186,420")).toBeVisible();
  await expect(page.locator("canvas")).toHaveCount(3);
  const chartSizes = await page.locator("canvas").evaluateAll((nodes) => nodes.map((node) => ({ width: node.clientWidth, height: node.clientHeight })));
  expect(chartSizes.every((size) => size.width > 200 && size.height > 200)).toBeTruthy();
  await page.screenshot({ path: "/tmp/xlwms-dashboard-desktop.png", fullPage: true });
});

test("mobile navigation reaches all inventory views", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockAPI(page);
  await page.goto("./");
  await expect(page.getByRole("heading", { name: "运营总览" })).toBeVisible();
  await page.getByTitle("打开导航").click();
  await expect(page.getByRole("navigation", { name: "主导航" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Temu 履约台" })).toHaveAttribute("href", "/temu/");
  await page.getByRole("button", { name: "库存中心" }).click();
  await expect(page.getByRole("heading", { name: "库存中心" })).toBeVisible();
  await expect(page.getByRole("tablist").getByRole("button")).toHaveCount(8);
  await expect(page.getByText("DEMO-SKU-01")).toBeVisible();
  await expect(page.locator("body")).not.toHaveCSS("overflow-x", "scroll");
  await page.screenshot({ path: "/tmp/xlwms-inventory-mobile.png", fullPage: true });
});

test("inventory warnings include inline warehouse SKU thresholds", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockAPI(page);
  await page.goto("./inventory-alerts");
  await expect(page.getByRole("heading", { name: "库存警告" })).toBeVisible();
  await expect(page.getByLabel("默认库存告警线")).toHaveValue("100");
  await expect(page.getByText("DEMO-SKU-01")).toBeVisible();
  await expect(page.getByText("需关注")).toBeVisible();
  await page.getByRole("button", { name: "全部 SKU 配置" }).click();
  await expect(page.getByText("DEMO-SKU-02")).toBeVisible();
  const input = page.getByLabel("EAST-01 DEMO-SKU-01 告警线");
  await input.fill("90");
  const saveRequest = page.waitForRequest((request) => request.url().endsWith("/inventory-alerts/config") && request.method() === "PATCH");
  await page.locator("tr", { hasText: "DEMO-SKU-01" }).getByTitle("保存告警线").click();
  expect((await saveRequest).postDataJSON()).toMatchObject({ wh_code: "EAST-01", warehouse_sku: "DEMO-SKU-01", threshold: 90 });
  await expect(page.getByText(/告警线已保存/)).toBeVisible();
  await expect(page.locator("body")).not.toHaveCSS("overflow-x", "scroll");
  const viewport = await page.evaluate(() => ({ pageWidth: document.documentElement.scrollWidth, viewportWidth: window.innerWidth }));
  expect(viewport.pageWidth).toBeLessThanOrEqual(viewport.viewportWidth);
  await page.screenshot({ path: "/tmp/xlwms-inventory-alerts-mobile.png", fullPage: true });
});

test("outbound workspace renders parcel orders", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 860 });
  await page.addInitScript(() => localStorage.setItem("xlwms-warehouse", "EAST-01"));
  await mockAPI(page);
  await page.goto("./outbound");
  await expect(page.getByRole("heading", { name: "出库管理" })).toBeVisible();
  await expect(page.getByText("OB-DEMO-1001")).toBeVisible();
  await expect(page.getByText("PO-DEMO-1001")).toBeVisible();
  await expect(page.getByText("仓库处理中")).toBeVisible();
  const fundsRequests: string[] = [];
  page.on("request", request => { if (request.url().includes("/funds-flows")) fundsRequests.push(request.url()); });
  const indexedRequest = page.waitForRequest(request => request.url().includes("/outbound-orders?") && request.url().includes("q=PO-DEMO-1001"));
  await page.getByPlaceholder("平台单号或出库单号").fill("PO-DEMO-1001");
  await page.getByRole("button", { name: "查询" }).click();
  const request = await indexedRequest;
  expect(request.method()).toBe("GET");
  await expect(page.getByText("PO-DEMO-1001")).toBeVisible();
  await expect(page.getByText(/最近查询/)).toBeVisible();
  expect(fundsRequests).toHaveLength(0);
  await page.screenshot({ path: "/tmp/xlwms-outbound-desktop.png", fullPage: true });
});

test("pending platform orders show live details without a warehouse filter", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockAPI(page);
  await page.goto("./platform-orders");
  await expect(page.getByRole("heading", { name: "平台订单待处理" })).toBeVisible();
  await expect(page.getByText("2,222", { exact: true })).toBeVisible();
  await expect(page.getByText("OMS-DEMO-2201")).toBeVisible();
  await expect(page.getByText("PO-DEMO-2201")).toBeVisible();
  await expect(page.getByText("WAREHOUSE-SKU-1")).toBeVisible();
  await expect(page.locator(".warehouse-select")).toHaveCount(0);
  const accountSelect = page.getByLabel("OMS 账户");
  await expect(accountSelect).toHaveValue("arp");
  await expect(accountSelect.locator("option")).toHaveCount(2);
  const dpsRequest = page.waitForRequest((request) => new URL(request.url()).searchParams.get("account") === "warehouse:DPSCA004");
  await accountSelect.selectOption("warehouse:DPSCA004");
  expect((await dpsRequest).method()).toBe("GET");
  await expect(page.getByText("45,944", { exact: true })).toBeVisible();
  await page.screenshot({ path: "/tmp/xlwms-platform-account-switch-mobile.png", fullPage: true });
  const searchRequest = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return url.pathname.endsWith("/platform-orders/pending") && url.searchParams.get("q") === "PO-DEMO-2201" &&
      url.searchParams.get("account") === "warehouse:DPSCA004";
  });
  await page.getByLabel("平台单号搜索").fill("PO-DEMO-2201");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  expect((await searchRequest).method()).toBe("GET");
  await expect(page.getByText("查询结果")).toBeVisible();
  await expect(page.getByText("1", { exact: true }).first()).toBeVisible();
  await page.getByTitle("查看平台订单详情").click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByRole("dialog").getByText("UPS Ground")).toBeVisible();
  await expect(page.getByRole("dialog").getByText("仓库演示产品")).toBeVisible();
  await expect(page.locator("body")).not.toHaveCSS("overflow-x", "scroll");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await page.screenshot({ path: "/tmp/xlwms-platform-orders-mobile.png", fullPage: true });
});

test("selected platform orders use automatic warehouse routing and the configured OMS account", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockAPI(page);
  await page.goto("./platform-orders");
  await page.getByLabel("选择平台订单 PO-DEMO-2201").check();
  const previewRequest = page.waitForRequest((request) => request.url().endsWith("/platform-orders/routing-preview"));
  await page.getByRole("button", { name: "分配仓库和物流" }).click();
  expect((await previewRequest).postDataJSON()).toEqual({ platform_order_nos: ["PO-DEMO-2201"], account: "arp" });

  const dialog = page.getByRole("dialog", { name: "分配仓库和物流" });
  await expect(dialog).toBeVisible();
  const dialogBox = await dialog.boundingBox();
  expect(dialogBox).not.toBeNull();
  expect(dialogBox!.x).toBeGreaterThanOrEqual(0);
  expect(dialogBox!.y).toBeGreaterThanOrEqual(0);
  expect(dialogBox!.x + dialogBox!.width).toBeLessThanOrEqual(1280);
  await expect(dialog.getByText("上传物流面单")).toBeVisible();
  await expect(dialog.getByLabel("自动匹配实际发货仓库")).toBeVisible();
  await expect(dialog.getByText("按购面单结果匹配发货仓库")).toBeVisible();
  await expect(dialog.getByText("平台美东仓")).toBeVisible();
  await expect(dialog.getByText("美东演示仓")).toBeVisible();
  await expect(dialog.getByLabel("实际发货仓库", { exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel("OMS 操作账号")).toHaveCount(0);
  await expect(dialog.getByLabel("OMS 操作密码")).toHaveCount(0);
  await dialog.getByRole("radio", { name: "Other" }).check();
  await dialog.getByRole("checkbox", { name: /我确认按以上购面单仓库/ }).check();
  let postActionRefreshes = 0;
  page.on("request", (request) => {
    if (request.method() === "GET" && request.url().includes("/platform-orders/pending")) postActionRefreshes++;
  });
  await page.screenshot({ path: "/tmp/xlwms-platform-routing-desktop.png", fullPage: true });

  const operationRequest = page.waitForRequest((request) => request.url().endsWith("/platform-orders/assign-and-approve") && request.method() === "POST");
  await dialog.getByRole("button", { name: "确定并审核" }).click();
  const request = await operationRequest;
  expect(request.headers().authorization).toBeUndefined();
  expect(request.postDataJSON()).toEqual({
    platform_order_nos: ["PO-DEMO-2201"],
    account: "arp",
    logistics_carrier: "other",
    confirmation: "CONFIRM_AND_APPROVE"
  });
  await expect(page.getByText("物流匹配已完成")).toBeVisible();
  await expect(page.getByText("成功 1 单，失败 0 单 · HYTX30")).toBeVisible();
  await expect.poll(() => postActionRefreshes, { timeout: 3500 }).toBeGreaterThanOrEqual(2);
});

test("parcel search uses only the watermarked local index after a miss", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 860 });
  await page.addInitScript(() => localStorage.setItem("xlwms-warehouse", "EAST-01"));
  await mockAPI(page, { indexedParcelMatch: false });
  const requests: string[] = [];
  page.on("request", request => {
    if (request.url().includes("/outbound-orders")) requests.push("outbound-index");
    if (request.url().includes("/funds-flows")) requests.push("funds-flow");
    if (request.url().includes("/outbound/parcel-list") && request.postDataJSON()?.data?.outboundOrderNos) requests.push("outbound-filter");
  });
  await page.goto("./outbound");
  await page.getByPlaceholder("平台单号或出库单号").fill("PO-DEMO-1001");
  await page.getByRole("button", { name: "查询" }).click();
  await expect(page.getByText("当前筛选暂无出库单")).toBeVisible();
  expect(requests).toEqual(["outbound-index"]);
});

test("fulfillment audit shows asynchronous progress and OMS matches", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockAPI(page);
  await page.goto("./fulfillment-audits");
  await expect(page.getByRole("heading", { name: "履约核查" })).toBeVisible();
  const auditFilters = page.locator(".audit-filters");
  await expect(auditFilters.getByLabel("选择仓库")).toBeVisible();
  await expect(auditFilters.getByLabel("选择 OMS 状态")).toBeVisible();
  await expect(page.locator(".warehouse-select")).toHaveCount(0);
  await expect(page.getByText(/最近查询/)).toBeVisible();
  await expect(page.getByText("待查询", { exact: true }).first()).toBeVisible();
  await expect(page.getByText("20", { exact: true })).toBeVisible();
  await expect(page.getByText("PO-DEMO-1001")).toBeVisible();
  await expect(page.getByRole("table").getByText("已取消", { exact: true })).toBeVisible();
  await expect(page.getByRole("table").getByText("领星出库单已取消", { exact: true })).toBeVisible();
  const downloadPromise = page.waitForEvent("download");
  await page.getByTitle("按仓库拆分导出人工订单 ZIP").click();
  await expect.poll(async () => (await downloadPromise).suggestedFilename()).toBe("manual-fulfillment-orders-demo.zip");
  await auditFilters.getByLabel("选择仓库").selectOption("EAST-01");
  const singleWarehouseDownload = page.waitForEvent("download");
  await page.getByTitle("导出当前仓库人工订单 CSV").click();
  await expect.poll(async () => (await singleWarehouseDownload).suggestedFilename()).toBe("manual-fulfillment-orders-demo.csv");
  await expect(page.locator("body")).not.toHaveCSS("overflow-x", "scroll");
});

test("cost filters combine date range and warehouse across both tabs", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockAPI(page);
  await page.goto("./costs");
  await expect(page.getByRole("heading", { name: "费用中心" })).toBeVisible();
  const filters = page.locator(".cost-filters");
  await expect(filters.getByLabel("开始日期")).toBeVisible();
  await expect(filters.getByLabel("结束日期")).toBeVisible();
  await expect(filters.getByLabel("选择仓库")).toBeVisible();
  await expect(page.locator(".warehouse-select")).toHaveCount(0);

  await filters.getByLabel("开始日期").fill("2026-08-01");
  await filters.getByLabel("结束日期").fill("2026-08-05");
  const fundsRequest = page.waitForRequest(request => {
    const url = new URL(request.url());
    return url.pathname.endsWith("/funds-flows") && url.searchParams.get("warehouse") === "EAST-01";
  });
  await filters.getByLabel("选择仓库").selectOption("EAST-01");
  const fundsURL = new URL((await fundsRequest).url());
  expect(fundsURL.searchParams.get("start_date")).toBe("2026-08-01");
  expect(fundsURL.searchParams.get("end_date")).toBe("2026-08-05");

  const detailsRequest = page.waitForRequest(request => request.url().includes("/cost-details?"));
  await page.getByRole("button", { name: "费用明细" }).click();
  const detailsURL = new URL((await detailsRequest).url());
  expect(detailsURL.searchParams.get("warehouse")).toBe("EAST-01");
  expect(detailsURL.searchParams.get("start_date")).toBe("2026-08-01");
  expect(detailsURL.searchParams.get("end_date")).toBe("2026-08-05");
  await expect(page.getByText("COST-DEMO-1")).toBeVisible();
  await expect(page.locator("body")).not.toHaveCSS("overflow-x", "scroll");
  await page.screenshot({ path: "/tmp/xlwms-costs-mobile.png", fullPage: true });
});

test("outbound tracking classifies pickup exceptions and combines filters", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 860 });
  await mockAPI(page);
  await page.goto("./fulfilled-orders");
  await expect(page.getByRole("heading", { name: "出库物流跟踪" })).toBeVisible();
  const filters = page.locator(".audit-filters");
  await expect(filters.getByLabel("选择店铺")).toBeVisible();
  await expect(filters.getByLabel("选择仓库")).toBeVisible();
  await expect(page.locator(".warehouse-select")).toHaveCount(0);
  const shopBox = await filters.getByLabel("选择店铺").boundingBox();
  const warehouseBox = await filters.getByLabel("选择仓库").boundingBox();
  expect(Math.abs((shopBox?.y ?? 0) - (warehouseBox?.y ?? 100))).toBeLessThan(2);
  await expect(page.getByText("PO-ARCHIVED-1002")).toBeVisible();
  await expect(page.getByText("OB-ARCHIVED-1002")).toBeVisible();
  await expect(page.locator(".audit-summary-item").filter({ hasText: "全部已出库" })).toContainText("2,007");
  await expect(page.getByText("出库满 24 小时仍未显示已揽收")).toBeVisible();
  await expect(page.getByText("待承运商揽收", { exact: true }).last()).toBeVisible();
  await expect(page.getByText("LAST-MILE-1002")).toBeVisible();
  await expect(page.locator(".page-header p")).toContainText("最近追踪");

  const exceptionSummary = page.locator(".audit-summary-item").filter({ hasText: "揽收异常订单" });
  await expect(exceptionSummary).toContainText("82");
  const categoryRequest = page.waitForRequest(request => new URL(request.url()).searchParams.get("tracking_category") === "pickup_exception");
  await exceptionSummary.click();
  expect(new URL((await categoryRequest).url()).searchParams.get("tracking_category")).toBe("pickup_exception");

  const shopRequest = page.waitForRequest(request => new URL(request.url()).searchParams.get("shop") === "panda-buy");
  await filters.getByLabel("选择店铺").selectOption("panda-buy");
  await shopRequest;
  const warehouseRequest = page.waitForRequest(request => new URL(request.url()).searchParams.get("warehouse") === "EAST-01");
  await filters.getByLabel("选择仓库").selectOption("EAST-01");
  const combinedURL = new URL((await warehouseRequest).url());
  expect(combinedURL.searchParams.get("shop")).toBe("panda-buy");
  expect(combinedURL.searchParams.get("tracking_category")).toBe("pickup_exception");
  await page.screenshot({ path: "/tmp/xlwms-outbound-tracking-desktop.png", fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".sidebar")).toHaveCSS("transform", "matrix(1, 0, 0, 1, -232, 0)");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await expect(page.getByRole("heading", { name: "出库物流跟踪" })).toBeVisible();
  await page.screenshot({ path: "/tmp/xlwms-outbound-tracking-mobile.png", fullPage: true });
});

test("warehouse page configures an encrypted OMS shipping account", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 860 });
  await mockAPI(page);
  await page.goto("./warehouses");

  await expect(page.getByRole("heading", { name: "仓库管理" })).toBeVisible();
  await expect(page.getByText("op***01")).toBeVisible();
  await expect(page.getByText("未绑定发货账号")).toBeVisible();
  await page.getByTitle("更换 OMS 发货账号").click();

  const dialog = page.getByRole("dialog", { name: "OMS 发货账号" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("华东一号仓 · EAST-01")).toBeVisible();
  await dialog.getByLabel("OMS 用户名").fill("warehouse-operator");
  await dialog.getByLabel("OMS 密码").fill("test-password");

  const saveRequest = page.waitForRequest(request => request.method() === "PUT" && request.url().endsWith("/warehouses/EAST-01/oms-account"));
  await page.screenshot({ path: "/tmp/xlwms-warehouse-account-desktop.png", fullPage: true });
  await dialog.getByRole("button", { name: "保存账号" }).click();
  expect((await saveRequest).postDataJSON()).toEqual({ username: "warehouse-operator", password: "test-password" });
  await expect(dialog).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
});
