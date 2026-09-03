import { expect, test, type Page, type Route } from "@playwright/test";

const carriers = ["GOFO", "SWIFTX", "SPEEDX", "YANWEN", "UPS", "USPS", "FEDEX", "UNIUNI"];

function carrierGroups(warehouseSKU = "") {
  return ["DPS002", "ARP_EAST"].map((warehouseKey, warehouseIndex) => ({
    warehouse_key: warehouseKey,
    warehouse_sku: warehouseSKU || undefined,
    customized: Boolean(warehouseSKU && warehouseIndex === 0),
    source: warehouseSKU && warehouseIndex === 0 ? "platform_sku" : "platform_default",
    base_rules: {
      warehouse_key: warehouseKey,
      allowed_carrier_codes: carriers.slice(0, 7),
      allow_signature: false,
      allowed_currency_codes: ["USD"],
      selection_mode: warehouseIndex === 0 ? "lowest_price" : "carrier_priority_within_delta",
      max_price_delta: warehouseIndex === 0 ? 0 : 0.5,
      warehouse_tie_priority: warehouseIndex + 1
    },
    carriers: carriers.slice(0, 7).map((carrierCode, index) => ({ warehouse_key: warehouseKey, carrier_code: carrierCode, priority: index + 1, enabled: true }))
  }));
}

async function fulfill(route: Route, data: unknown) {
  await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ success: true, data }) });
}

async function mockPolicyAPI(page: Page) {
  await page.route("**/warehouse-console/healthz", (route) => fulfill(route, { status: "ok" }));
  await page.route("**/warehouse-console/api/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    if (path.endsWith("/warehouses")) return fulfill(route, []);
    if (path.endsWith("/fulfillment-policies/carriers")) return fulfill(route, carrierGroups(url.searchParams.get("warehouse_sku") || ""));
    if (/\/fulfillment-policies\/carriers\/[^/]+$/.test(path)) {
      const warehouseKey = decodeURIComponent(path.split("/").at(-1) || "");
      const payload = route.request().postDataJSON();
      const source = carrierGroups(url.searchParams.get("warehouse_sku") || "").find((group) => group.warehouse_key === warehouseKey)!;
      return fulfill(route, { ...source, base_rules: payload.base_rules || source.base_rules, carriers: payload.carriers || source.carriers });
    }
    if (path.endsWith("/fulfillment-policies/skus")) return fulfill(route, {
      platform: url.searchParams.get("platform") || "temu",
      records: [{ platform: "temu", warehouse_sku: "DEMO-SKU-01", product_name: "演示收纳篮", disabled_warehouse_keys: ["ARP_EAST"], customized: true, updated_at: "2026-09-03T08:00:00Z" }],
      total: 1, page: 1, page_size: 30, pages: 1
    });
    return fulfill(route, {});
  });
}

test("legacy shipping policy route opens the base-rule directory", async ({ page }) => {
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies");
  await expect(page).toHaveURL(/\/shipping-policies\/base-rules$/);
  await expect(page.getByRole("heading", { name: "基础快递限制", level: 1 })).toBeVisible();
  await expect(page.locator(".warehouse-policy-card")).toHaveCount(2);
  await expect(page.locator(".carrier-allow-grid")).toHaveCount(2);
  await expect(page.locator(".carrier-priority-list")).toHaveCount(0);
  await page.screenshot({ path: "/tmp/xlwms-policy-base-desktop.png", fullPage: true });
});

test("policy subdirectories isolate selection and SKU settings", async ({ page }) => {
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/base-rules");

  await page.locator(".policy-view-nav").getByRole("button", { name: "快递选择算法" }).click();
  await expect(page).toHaveURL(/\/shipping-policies\/selection$/);
  await expect(page.getByRole("heading", { name: "快递选择算法", level: 1 })).toBeVisible();
  await expect(page.locator(".carrier-priority-list")).toHaveCount(2);
  await expect(page.locator(".carrier-allow-grid")).toHaveCount(0);
  await page.screenshot({ path: "/tmp/xlwms-policy-selection-desktop.png", fullPage: true });

  await page.locator(".policy-view-nav").getByRole("button", { name: "SKU 发货规则" }).click();
  await expect(page).toHaveURL(/\/shipping-policies\/sku-rules$/);
  await expect(page.getByText("DEMO-SKU-01")).toBeVisible();
  await expect(page.getByText("DPS002")).toBeVisible();
  await expect(page.getByText("ARP_EAST")).toHaveCount(0);

  await page.getByRole("button", { name: "编辑" }).click();
  await expect(page.getByRole("dialog", { name: "DEMO-SKU-01" })).toBeVisible();
  await expect(page.locator(".warehouse-toggle-grid label")).toHaveCount(2);
  await expect(page.locator(".sku-carrier-card")).toHaveCount(2);
  await page.screenshot({ path: "/tmp/xlwms-policy-sku-dialog-desktop.png", fullPage: true });
});

test("mobile policy directory remains usable without horizontal overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/selection");
  await page.getByTitle("打开导航").click();
  await expect(page.locator(".nav-submenu").getByRole("button")).toHaveCount(3);
  await page.screenshot({ path: "/tmp/xlwms-policy-directory-mobile.png" });
  await page.locator(".nav-submenu").getByRole("button", { name: "基础快递限制" }).click();
  await expect(page.getByRole("heading", { name: "基础快递限制", level: 1 })).toBeVisible();
  const sizes = await page.evaluate(() => ({ viewport: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }));
  expect(sizes.scrollWidth).toBeLessThanOrEqual(sizes.viewport);
});
