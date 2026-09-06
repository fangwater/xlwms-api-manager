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

async function mockPolicyAPI(page: Page, skuStatus = "ready") {
  const accounts = [
    { key: "arp", label: "FHZARP-衣架", username_hint: "FH***RP", enabled: true, api_credential_keys: ["api-hanger"], sku_count: 8, updated_at: "2026-09-03T08:00:00Z" },
    { key: "dps", label: "FHZDPS-衣架", username_hint: "FH***PS", enabled: true, api_credential_keys: ["api-dps"], sku_count: 6, updated_at: "2026-09-03T08:00:00Z" }
  ];
  const accountHealth = [
    { key: "arp", label: "FHZARP-衣架", username_hint: "FH***RP", api_credential_keys: ["api-hanger"], available: false, status: "mfa_required", error: "需要短信、邮箱或验证器二次验证" },
    { key: "dps", label: "FHZDPS-衣架", username_hint: "FH***PS", api_credential_keys: ["api-dps"], available: true, status: "ready" }
  ];
  const credentials = [
    { key: "api-hanger", label: "衣架 OpenAPI", api_base_url: "https://api.example", app_key_hint: "aa***01", warehouse_codes: ["HYTX30", "ARPCA01"], sku_count: 8, oms_account_key: "arp", oms_account_label: "FHZARP-衣架", active: true, deletable: false, updated_at: "2026-09-03T08:00:00Z" },
    { key: "api-dps", label: "DPS OpenAPI", api_base_url: "https://api.example", app_key_hint: "bb***02", warehouse_codes: ["DPSNY002", "DPSCA004"], sku_count: 6, oms_account_key: "dps", oms_account_label: "FHZDPS-衣架", active: true, deletable: false, updated_at: "2026-09-03T08:00:00Z" },
    { key: "api-laundry", label: "脏衣篓 OpenAPI", api_base_url: "https://api.example", app_key_hint: "cc***03", warehouse_codes: ["HYTX30", "ARPCA01"], sku_count: 13, oms_account_key: "", oms_account_label: "", active: true, deletable: true, updated_at: "2026-09-03T08:00:00Z" }
  ];
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
        const created = { key: payload.key, label: payload.label, username_hint: "NE***ER", enabled: true, api_credential_keys: payload.api_credential_keys, sku_count: 13, updated_at: "2026-09-03T08:00:00Z" };
        accounts.push(created);
        accountHealth.push({ key: payload.key, label: payload.label, username_hint: "NE***ER", api_credential_keys: payload.api_credential_keys, available: true, status: "ready", error: "" });
        for (const credential of credentials) {
          if (payload.api_credential_keys.includes(credential.key)) {
            credential.oms_account_key = payload.key;
            credential.oms_account_label = payload.label;
          }
        }
        return fulfill(route, created);
      }
      return fulfill(route, accounts.map((account) => ({ ...account, sku_status: skuStatus })));
    }
    if (/\/warehouse-api-credentials\/[^/]+\/sync$/.test(path)) {
      skuStatus = "ready";
      return fulfill(route, { synced: true });
    }
    if (path.endsWith("/platform-orders/accounts")) return fulfill(route, accountHealth);
    if (/\/platform-orders\/accounts\/[^/]+\/mfa-challenge$/.test(path)) {
      return fulfill(route, { channel: "TOTP", masked_target: "Authenticator", code_sent: false, code_length: 6 });
    }
    if (/\/platform-orders\/accounts\/[^/]+\/mfa-verify$/.test(path)) {
      const accountKey = decodeURIComponent(path.split("/").at(-2) || "");
      const item = accountHealth.find((account) => account.key === accountKey)!;
      item.available = true;
      item.status = "ready";
      item.error = "";
      return fulfill(route, accountHealth);
    }
    if (path.endsWith("/warehouse-api-credentials")) return fulfill(route, credentials);
    if (/\/fulfillment-policies\/accounts\/[^/]+\/api-credentials$/.test(path)) {
      const accountKey = decodeURIComponent(path.split("/").at(-2) || "");
      const payload = route.request().postDataJSON();
      const account = accounts.find((item) => item.key === accountKey)!;
      account.api_credential_keys = payload.api_credential_keys;
      return fulfill(route, account);
    }
    if (/\/fulfillment-policies\/accounts\/[^/]+$/.test(path)) {
      const accountKey = decodeURIComponent(path.split("/").at(-1) || "");
      const payload = route.request().postDataJSON();
      const account = accounts.find((item) => item.key === accountKey)!;
      Object.assign(account, { label: payload.label || account.label, enabled: payload.enabled ?? account.enabled });
      return fulfill(route, account);
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

test("account management creates accounts with OpenAPI scope bindings", async ({ page }) => {
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/accounts");
  await expect(page.getByRole("heading", { name: "OMS 账号管理", level: 1 })).toBeVisible();
  await expect(page.locator(".account-policy-card")).toHaveCount(2);
  await expect(page.getByText("衣架 OpenAPI")).toBeVisible();
  await page.getByRole("button", { name: "新建账户" }).click();
  await page.getByLabel("账户标识").fill("backup");
  await page.getByLabel("显示名称").fill("备用账户");
  await page.getByLabel("OMS 账号").fill("new-user");
  await page.getByLabel("OMS 密码").fill("new-password");
  await page.getByText("脏衣篓 OpenAPI").click();
  await expect(page.getByText("验证成功后加密保存，API 凭据决定该账号负责的 SKU 范围")).toBeVisible();
  await page.getByRole("button", { name: "验证并新建" }).click();
  await expect(page.locator(".account-policy-card")).toHaveCount(3);
  await expect(page.getByRole("heading", { name: "备用账户", level: 3 })).toBeVisible();
  await page.screenshot({ path: "/tmp/xlwms-account-management-desktop.png", fullPage: true });
});

test("account management reassigns an OpenAPI scope", async ({ page }) => {
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/accounts");
  const account = page.locator(".account-policy-card", { hasText: "FHZARP-衣架" });
  await account.getByRole("button", { name: "绑定 API" }).click();
  await page.getByText("脏衣篓 OpenAPI").click();
  await page.getByRole("button", { name: "保存绑定" }).click();
  await expect(account.getByText("2 组 OpenAPI")).toBeVisible();
  await page.screenshot({ path: "/tmp/xlwms-account-bindings-desktop.png", fullPage: true });
});

test("account workspace filters accounts without redundant counters or warehouse scope", async ({ page }) => {
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/accounts");
  await expect(page.locator(".account-policy-card")).toHaveCount(2);
  await expect(page.getByText("个 OMS 发货账户")).toHaveCount(0);
  await expect(page.getByLabel("当前仓库")).toHaveCount(0);
  await page.getByLabel("搜索账户", { exact: true }).fill("DPS OpenAPI");
  await expect(page.locator(".account-policy-card")).toHaveCount(1);
  await expect(page.getByRole("heading", { name: "FHZDPS-衣架" })).toBeVisible();
  await page.getByLabel("搜索账户", { exact: true }).fill("no-matching-account");
  await expect(page.getByText("没有匹配的账户")).toBeVisible();
  await page.getByLabel("搜索账户", { exact: true }).clear();
  await expect(page.locator(".account-policy-card")).toHaveCount(2);
});

test("account SKU sync distinguishes pending and failed from zero", async ({ page }) => {
  await mockPolicyAPI(page, "pending");
  await page.goto("./accounts");
  const account = page.locator(".account-policy-card", { hasText: "FHZARP-衣架" });
  await expect(account.getByText("SKU 待同步")).toBeVisible();
  await expect(account.getByText("0 个 SKU", { exact: true })).toHaveCount(0);
  await account.getByTitle("同步 SKU").click();
  await expect(account.getByText("8 个 SKU", { exact: true })).toBeVisible();
  await page.route("**/warehouse-console/api/fulfillment-policies/accounts?*", (route) => fulfill(route, [{ key: "arp", label: "FHZARP-衣架", api_credential_keys: ["api-hanger"], sku_count: 8, sku_status: "failed", enabled: true }]));
  await page.reload();
  await expect(account.getByText("SKU 同步失败")).toBeVisible();
  await expect(account.getByText("0 个 SKU", { exact: true })).toHaveCount(0);
});

test("account management completes six-digit OMS verification", async ({ page }) => {
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/accounts");
  const account = page.locator(".account-policy-card", { hasText: "FHZARP-衣架" });
  await expect(account.getByText("需要短信、邮箱或验证器二次验证")).toBeVisible();
  await account.getByRole("button", { name: "二次验证" }).click();
  await expect(page.getByRole("dialog", { name: "FHZARP-衣架 二次验证" })).toBeVisible();
  await page.getByLabel("6 位验证码").fill("123456");
  await page.getByRole("button", { name: "验证登录" }).click();
  await expect(account.getByText("登录正常")).toBeVisible();
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

test("mobile account management remains usable without horizontal overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockPolicyAPI(page);
  await page.goto("./shipping-policies/accounts");
  await expect(page.locator(".account-policy-card")).toHaveCount(2);
  await expect(page).toHaveURL(/\/accounts$/);
  await expect(page.locator(".policy-view-nav")).toHaveCount(0);
  await page.screenshot({ path: "/tmp/xlwms-account-management-mobile.png", fullPage: true });
  const sizes = await page.evaluate(() => ({ viewport: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }));
  expect(sizes.scrollWidth).toBeLessThanOrEqual(sizes.viewport);
});
