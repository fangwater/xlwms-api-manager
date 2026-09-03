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
    if (path.endsWith("/warehouses")) return fulfill(route, [
      { wh_code: "DPSNY002", name: "DPS 美东", region: "east", active: true },
      { wh_code: "DPSCA004", name: "DPS 美西", region: "west", active: true },
      { wh_code: "HYTX30", name: "ARP 美东", region: "east", active: true }
    ]);
    if (path.endsWith("/fulfillment-policies/accounts")) {
      if (route.request().method() === "POST") {
        const payload = route.request().postDataJSON();
        return fulfill(route, { key: payload.key, label: payload.label, username_hint: "NE***ER", enabled: true, warehouse_codes: payload.warehouse_codes, route_count: 0, updated_at: "2026-09-03T08:00:00Z" });
      }
      return fulfill(route, [
        { key: "arp", label: "ARP 账户", username_hint: "FH***RP", enabled: true, warehouse_codes: ["HYTX30", "DPSNY002"], route_count: 1, updated_at: "2026-09-03T08:00:00Z" },
        { key: "dps", label: "DPS 账户", username_hint: "FH***PS", enabled: true, warehouse_codes: ["DPSNY002", "DPSCA004"], route_count: 0, updated_at: "2026-09-03T08:00:00Z" }
      ]);
    }
    if (/\/fulfillment-policies\/accounts\/[^/]+$/.test(path)) {
      const accountKey = decodeURIComponent(path.split("/").at(-1) || "");
      const payload = route.request().postDataJSON();
      return fulfill(route, { key: accountKey, label: payload.label || `${accountKey.toUpperCase()} 账户`, username_hint: "FH***NT", enabled: payload.enabled ?? true, warehouse_codes: ["HYTX30"], route_count: 0, updated_at: "2026-09-03T08:00:00Z" });
    }
    if (path.endsWith("/fulfillment-policies/account-routes")) return fulfill(route, {
      platform: url.searchParams.get("platform") || "temu",
      records: [
        { platform: "temu", warehouse_sku: "DEMO-SKU-01", product_name: "演示收纳篮", account_key: "arp", account_label: "ARP 账户", configured: true, updated_at: "2026-09-03T08:00:00Z" },
        { platform: "temu", warehouse_sku: "DEMO-SKU-02", product_name: "演示衣架", configured: false, updated_at: "2026-09-03T08:00:00Z" }
      ],
      total: 2, page: 1, page_size: 30, pages: 1
    });
    if (/\/fulfillment-policies\/account-routes\/[^/]+$/.test(path)) {
      const sku = decodeURIComponent(path.split("/").at(-1) || "");
      const payload = route.request().postDataJSON();
      return fulfill(route, { platform: url.searchParams.get("platform") || "temu", warehouse_sku: sku, product_name: "演示衣架", account_key: payload.account_key, account_label: payload.account_key === "dps" ? "DPS 账户" : "ARP 账户", configured: true, updated_at: "2026-09-03T08:00:00Z" });
    }
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

test("account management creates accounts and supports overlapping warehouses", async ({ page }) => {
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/accounts");
  await expect(page.getByRole("heading", { name: "OMS 账号管理", level: 1 })).toBeVisible();
  await expect(page.locator(".account-policy-card")).toHaveCount(2);
  await expect(page.getByText("DPSNY002")).toHaveCount(2);
  await page.getByRole("button", { name: "新建账户" }).click();
  await page.getByLabel("账户标识").fill("backup");
  await page.getByLabel("显示名称").fill("备用账户");
  await page.getByLabel("OMS 账号").fill("new-user");
  await page.getByLabel("OMS 密码").fill("new-password");
  await page.locator(".account-create-warehouses label").first().click();
  await page.getByRole("button", { name: "验证并新建" }).click();
  await expect(page.locator(".account-policy-card")).toHaveCount(3);
  await expect(page.getByRole("heading", { name: "备用账户", level: 3 })).toBeVisible();
  await page.screenshot({ path: "/tmp/xlwms-account-management-desktop.png", fullPage: true });
});

test("account routing assigns per-SKU ownership", async ({ page }) => {
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/account-routes");
  await expect(page.getByRole("heading", { name: "OMS 账户路由", level: 1 })).toBeVisible();
  await expect(page.getByText("DEMO-SKU-02")).toBeVisible();
  await page.getByLabel("DEMO-SKU-02 OMS 发货账户").selectOption("dps");
  await expect(page.getByLabel("DEMO-SKU-02 OMS 发货账户")).toHaveValue("dps");
  await expect(page.locator(".account-route-table .status-badge", { hasText: "已配置" })).toHaveCount(2);
  await page.screenshot({ path: "/tmp/xlwms-account-routes-desktop.png", fullPage: true });
});

test("mobile policy directory remains usable without horizontal overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/selection");
  await page.getByTitle("打开导航").click();
  await expect(page.locator(".nav-submenu").getByRole("button")).toHaveCount(5);
  await page.screenshot({ path: "/tmp/xlwms-policy-directory-mobile.png" });
  await page.locator(".nav-submenu").getByRole("button", { name: "基础快递限制" }).click();
  await expect(page.getByRole("heading", { name: "基础快递限制", level: 1 })).toBeVisible();
  const sizes = await page.evaluate(() => ({ viewport: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }));
  expect(sizes.scrollWidth).toBeLessThanOrEqual(sizes.viewport);
});

test("mobile account management remains usable without horizontal overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/accounts");
  await expect(page.locator(".account-policy-card")).toHaveCount(2);
  await page.screenshot({ path: "/tmp/xlwms-account-management-mobile.png", fullPage: true });
  const sizes = await page.evaluate(() => ({ viewport: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }));
  expect(sizes.scrollWidth).toBeLessThanOrEqual(sizes.viewport);
});
