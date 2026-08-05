import { expect, test, type Page } from "@playwright/test";

const warehouses = [
  { wh_code: "EAST-01", name: "华东一号仓", api_base_url: "https://api.xlwms.com/openapi", app_key_hint: "demo...key", active: true, updated_at: "2026-08-01T08:00:00Z" },
  { wh_code: "WEST-02", name: "西部分拨仓", api_base_url: "https://api.xlwms.com/openapi", app_key_hint: "demo...key", active: true, updated_at: "2026-08-01T08:00:00Z" }
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
    else if (path.endsWith("/inventory")) data = { records: [], total: 0, page: 1, page_size: 30, pages: 1 };
    else if (path.endsWith("/fulfillment-audits/archived")) data = {
      records: [{
        id: 2, platform: "temu", shop_code: "panda-buy", shop_name: "PANDA BUY",
        platform_order_no: "PO-ARCHIVED-1002", platform_status: "pending_pickup", platform_status_code: 4,
        wh_code: "HYTX30", warehouse_key: "arp-east", tracking_number: "TRACK-ARCHIVED",
        oms_status: "outbound", oms_status_code: 3, outbound_order_no: "OB-ARCHIVED-1002",
        oms_tracking_number: "TRACK-ARCHIVED", exception_category: "archived", active: false,
        oms_outbound_at: "2026-08-05T06:40:00Z", updated_at: "2026-08-05T06:41:00Z"
      }],
      total: 2007, page: 1, page_size: 30, pages: 67, last_query_at: "2026-08-05T07:00:00Z",
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
    else if (path.endsWith("/outbound-orders")) {
      const records = options.indexedParcelMatch === false ? [] : [{ whCode: "EAST-01", outboundOrderNo: "OB-DEMO-1001", thirdOrderNo: "EXT-DEMO-88", platformOrderNo: "PO-DEMO-1001", status: 2, logisticsTrackNo: "TRACK-DEMO", orderCreateTime: "2026-08-01T01:30:00Z" }];
      data = { records, total: records.length, page: 1, page_size: 30, pages: 1, last_query_at: "2026-08-05T07:00:00Z" };
    }
    else if (path.endsWith("/funds-flows")) data = { records: [{ id: 1, wh_code: "EAST-01", order_no: "OB-DEMO-1001", platform_order_no: "PO-DEMO-1001" }], total: 1, page: 1, page_size: 100, pages: 1 };
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

test("archived fulfillment page lists outbound platform orders", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 860 });
  await mockAPI(page);
  await page.goto("./fulfilled-orders");
  await expect(page.getByRole("heading", { name: "已出库平台单" })).toBeVisible();
  await expect(page.getByText("PO-ARCHIVED-1002")).toBeVisible();
  await expect(page.getByText("OB-ARCHIVED-1002")).toBeVisible();
  await expect(page.getByText("2,007")).toBeVisible();
  await expect(page.getByText(/最近查询/)).toBeVisible();
});
