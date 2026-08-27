import { expect, test, type Page } from "@playwright/test";

type PairingRecord = {
  id: string;
  platform_sku: string;
  store_code: string;
  store_name: string;
  items: Array<{ system_sku: string; product_name: string; quantity: number; approve_status: number }>;
  created_at: string;
};

async function mockProductPairingAPI(page: Page) {
  const records: PairingRecord[] = [{
    id: "81",
    platform_sku: "20PCS-BUNDLE",
    store_code: "TEMU-US",
    store_name: "Temu 美国店",
    items: [{ system_sku: "10PCS", product_name: "10 件装", quantity: 2, approve_status: 2 }],
    created_at: "2026-08-27 10:20:30",
  }];
  await page.route("**/warehouse-console/healthz", (route) => route.fulfill({
    status: 200, contentType: "application/json", body: JSON.stringify({ success: true, data: { status: "ok" } }),
  }));
  await page.route("**/warehouse-console/api/**", (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    let data: unknown = {};
    let status = 200;
    if (path.endsWith("/warehouses")) data = [];
    else if (path.endsWith("/platform-orders/accounts")) data = [
      { key: "arp", label: "ARP 账户", warehouse_codes: ["ARPCA01"], available: true, status: "ready" },
      { key: "warehouse:DPSCA004", label: "DPS 账户", warehouse_codes: ["DPSCA004"], available: true, status: "ready" },
    ];
    else if (path.endsWith("/product-pairings") && route.request().method() === "GET") data = {
      account: url.searchParams.get("account"), records, total: records.length,
      page: 1, page_size: 30, pages: 1, queried_at: "2026-08-27T10:30:00Z",
    };
    else if (path.endsWith("/product-pairings") && route.request().method() === "POST") {
      const payload = route.request().postDataJSON();
      records.unshift({
        id: "82", platform_sku: payload.platform_sku, store_code: payload.store_code,
        store_name: payload.store_code, created_at: "2026-08-27 11:00:00",
        items: payload.items.map((item: { system_sku: string; quantity: number }) => ({ ...item, product_name: "", approve_status: 0 })),
      });
      status = 201;
      data = { account: payload.account, store_code: payload.store_code, platform_sku: payload.platform_sku, items: payload.items };
    } else if (path.endsWith("/product-pairings/delete") && route.request().method() === "POST") {
      const payload = route.request().postDataJSON();
      const index = records.findIndex((item) => item.store_code === payload.store_code && item.platform_sku === payload.platform_sku);
      if (index >= 0) records.splice(index, 1);
      data = { account: payload.account, store_code: payload.store_code, platform_sku: payload.platform_sku };
    }
    return route.fulfill({ status, contentType: "application/json", body: JSON.stringify({ success: true, data }) });
  });
}

test("combination pairings can be filtered, created, and deleted", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockProductPairingAPI(page);
  await page.goto("./product-pairings");

  await expect(page.getByRole("heading", { name: "组合配对" })).toBeVisible();
  await expect(page.getByText("20PCS-BUNDLE")).toBeVisible();
  await expect(page.getByText("10PCS")).toBeVisible();
  await expect(page.getByText("× 2")).toBeVisible();
  await expect(page.getByText("已审核", { exact: true })).toBeVisible();

  await page.getByLabel("店铺编码").fill("TEMU-US");
  await page.getByLabel("查询字段").selectOption("system_sku");
  await page.getByLabel("配对关键词").fill("10PCS");
  const filterRequest = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return request.method() === "GET" && url.pathname.endsWith("/product-pairings") && url.searchParams.get("store_code") === "TEMU-US" && url.searchParams.get("q") === "10PCS";
  });
  await page.getByRole("button", { name: "查询", exact: true }).click();
  expect(new URL((await filterRequest).url()).searchParams.get("query_field")).toBe("system_sku");

  await page.getByRole("button", { name: "新建配对" }).click();
  const dialog = page.getByRole("dialog", { name: "新建组合配对" });
  await dialog.getByLabel("店铺编码").fill("TEMU-US");
  await dialog.getByLabel("平台 SKU").fill("MIXED-BUNDLE");
  await dialog.getByLabel("系统 SKU 1").fill("PACK-A");
  await dialog.getByLabel("数量 1").fill("2");
  await dialog.getByRole("button", { name: "添加 SKU" }).click();
  await dialog.getByLabel("系统 SKU 2").fill("PACK-B");
  await dialog.getByLabel("数量 2").fill("1");
  const createRequest = page.waitForRequest((request) => request.method() === "POST" && request.url().endsWith("/product-pairings"));
  await dialog.getByRole("button", { name: "创建配对" }).click();
  expect((await createRequest).postDataJSON()).toEqual({
    account: "arp", store_code: "TEMU-US", platform_sku: "MIXED-BUNDLE",
    items: [{ system_sku: "PACK-A", quantity: 2 }, { system_sku: "PACK-B", quantity: 1 }],
  });
  await expect(page.getByText("组合配对“MIXED-BUNDLE”已创建")).toBeVisible();
  await expect(page.getByText("MIXED-BUNDLE", { exact: true })).toBeVisible();

  page.once("dialog", (confirm) => confirm.accept());
  const row = page.locator("tr", { hasText: "MIXED-BUNDLE" });
  const deleteRequest = page.waitForRequest((request) => request.method() === "POST" && request.url().endsWith("/product-pairings/delete"));
  await row.getByTitle("删除组合配对").click();
  expect((await deleteRequest).postDataJSON()).toEqual({ account: "arp", store_code: "TEMU-US", platform_sku: "MIXED-BUNDLE" });
  await expect(page.getByText("组合配对“MIXED-BUNDLE”已删除")).toBeVisible();
  await expect(row).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await page.screenshot({ path: "/tmp/xlwms-product-pairings-desktop.png", fullPage: true });
});

test("combination pairing editor fits a mobile viewport", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockProductPairingAPI(page);
  await page.goto("./product-pairings");
  await page.getByRole("button", { name: "新建配对" }).click();
  const dialog = page.getByRole("dialog", { name: "新建组合配对" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "添加 SKU" }).click();
  await expect(dialog.getByLabel("系统 SKU 2")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await page.screenshot({ path: "/tmp/xlwms-product-pairings-mobile.png", fullPage: true });
});
