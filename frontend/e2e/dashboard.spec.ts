import { expect, test, type Page } from "@playwright/test";

const warehouses = [
  { wh_code: "EAST-01", name: "华东一号仓", api_base_url: "https://api.xlwms.com/openapi", app_key_hint: "demo...key", active: true, updated_at: "2026-08-01T08:00:00Z" },
  { wh_code: "WEST-02", name: "西部分拨仓", api_base_url: "https://api.xlwms.com/openapi", app_key_hint: "demo...key", active: true, updated_at: "2026-08-01T08:00:00Z" }
];

async function mockAPI(page: Page) {
  await page.route("**/warehouse-console/healthz", (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ success: true, data: { status: "ok" } }) }));
  await page.route("**/warehouse-console/api/**", (route) => {
    const path = new URL(route.request().url()).pathname;
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
    else if (path.includes("/outbound/parcel-list")) data = { code: 200, msg: "", data: { records: [{ whCode: "EAST-01", outboundOrderNo: "OB-DEMO-1001", thirdOrderNo: "EXT-DEMO-88", status: 2, logisticsChannel: "DEMO-CHANNEL", logisticsTrackNo: "TRACK-DEMO", orderCreateTime: "2026-08-01 09:30:00" }], total: 1, page: 1, pageSize: 30, pages: 1 } };
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
  await expect(page.getByText("仓库处理中")).toBeVisible();
  await page.screenshot({ path: "/tmp/xlwms-outbound-desktop.png", fullPage: true });
});
