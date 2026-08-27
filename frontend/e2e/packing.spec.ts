import { expect, test, type Page } from "@playwright/test";

const skuSpecs = [
  {
    warehouse_sku: "PACK-A", product_name: "折叠收纳盒", length_cm: 20, width_cm: 10, height_cm: 10,
    weight_kg: 1.2, note: "", enabled: true, source: "manual", complete: true, missing_fields: [],
    first_seen_at: "2026-08-01T08:00:00Z", updated_at: "2026-08-01T08:00:00Z",
  },
  {
    warehouse_sku: "PACK-B", product_name: "桌面配件包", length_cm: 12, width_cm: 8, height_cm: 6,
    weight_kg: 0.8, note: "", enabled: true, source: "manual", complete: true, missing_fields: [],
    first_seen_at: "2026-08-01T08:00:00Z", updated_at: "2026-08-01T08:00:00Z",
  },
];

function packingPlan() {
  return {
    algorithm: "fixed-orientation-envelope-v1",
    heuristic: true,
    packages: [
      {
        index: 1,
        dimensions: { length_cm: 32, width_cm: 10, height_cm: 10 },
        placements: [
          {
            step: 1, unit_id: "PACK-A#1", warehouse_sku: "PACK-A", product_name: "折叠收纳盒",
            position: { x: 0, y: 0, z: 0 }, dimensions: { length_cm: 20, width_cm: 10, height_cm: 10 },
            original_dimensions: { length_cm: 20, width_cm: 10, height_cm: 10 }, weight_kg: 1.2,
          },
          {
            step: 2, unit_id: "PACK-B#1", warehouse_sku: "PACK-B", product_name: "桌面配件包",
            position: { x: 20, y: 0, z: 0 }, dimensions: { length_cm: 12, width_cm: 8, height_cm: 6 },
            original_dimensions: { length_cm: 12, width_cm: 8, height_cm: 6 }, weight_kg: 0.8,
          },
        ],
        packed_units: 2, used_weight_kg: 2, used_volume_cm3: 2576, volume_utilization_percent: 80.5,
      },
      {
        index: 2,
        dimensions: { length_cm: 20, width_cm: 10, height_cm: 10 },
        placements: [{
          step: 1, unit_id: "PACK-A#2", warehouse_sku: "PACK-A", product_name: "折叠收纳盒",
          position: { x: 0, y: 0, z: 0 }, dimensions: { length_cm: 20, width_cm: 10, height_cm: 10 },
          original_dimensions: { length_cm: 20, width_cm: 10, height_cm: 10 }, weight_kg: 1.2,
        }],
        packed_units: 1, used_weight_kg: 1.2, used_volume_cm3: 2000, volume_utilization_percent: 100,
      },
    ],
    unfit_items: [],
    summary: {
      requested_units: 3, packed_units: 3, unfit_units: 0, packages_used: 2,
      total_weight_kg: 3.2, packed_weight_kg: 3.2, packed_volume_cm3: 4576,
    },
  };
}

type CombinationMockOptions = {
  combinations?: Array<Record<string, unknown>>;
  onSave?: (method: string, payload: Record<string, unknown>) => void;
};

async function mockPackingAPI(page: Page, createPlan: () => unknown = packingPlan, combinationOptions: CombinationMockOptions = {}) {
  const combinations = combinationOptions.combinations ?? [];
  await page.route("**/warehouse-console/healthz", (route) => route.fulfill({
    status: 200, contentType: "application/json", body: JSON.stringify({ success: true, data: { status: "ok" } }),
  }));
  await page.route("**/warehouse-console/api/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    let data: unknown = {};
    if (path.endsWith("/warehouses")) data = [];
    else if (path.endsWith("/warehouse-sku-specs")) data = { records: skuSpecs, total: 2, page: 1, page_size: 30, pages: 1 };
    else if (path.endsWith("/packing/plans")) data = createPlan();
    else if (path.endsWith("/packing/combinations") && route.request().method() === "GET") data = combinations;
    else if (path.includes("/packing/combinations") && ["POST", "PUT"].includes(route.request().method())) {
      const payload = route.request().postDataJSON() as Record<string, unknown>;
      combinationOptions.onSave?.(route.request().method(), payload);
      const existingID = Number(path.split("/").at(-1)) || 41;
      const saved = {
        id: existingID, corrected: true, created_at: "2026-08-26T08:00:00Z", updated_at: "2026-08-26T09:00:00Z",
        ...payload,
        items: (payload.items as Array<Record<string, unknown>>).map((member) => ({
          ...member,
          product_name: member.warehouse_sku === "PACK-A" ? "折叠收纳盒" : "桌面配件包",
          length_cm: member.warehouse_sku === "PACK-A" ? 20 : 12,
          width_cm: member.warehouse_sku === "PACK-A" ? 10 : 8,
          height_cm: member.warehouse_sku === "PACK-A" ? 10 : 6,
          weight_kg: member.warehouse_sku === "PACK-A" ? 1.2 : 0.8,
        })),
      };
      const index = combinations.findIndex((item) => item.id === existingID);
      if (index >= 0) combinations[index] = saved; else combinations.push(saved);
      data = saved;
    } else if (path.includes("/packing/combinations") && route.request().method() === "DELETE") data = { deleted: true };
    return route.fulfill({ status: path.endsWith("/packing/combinations") && route.request().method() === "POST" ? 201 : 200, contentType: "application/json", body: JSON.stringify({ success: true, data }) });
  });
}

function singlePackagePlan() {
  const result = packingPlan();
  const secondPlacement = result.packages[1].placements[0];
  result.packages = [{
    ...result.packages[0],
    dimensions: { length_cm: 52, width_cm: 10, height_cm: 10 },
    placements: [...result.packages[0].placements, { ...secondPlacement, position: { x: 32, y: 0, z: 0 }, step: 3 }],
    packed_units: 3,
    used_weight_kg: 3.2,
    used_volume_cm3: 4576,
    volume_utilization_percent: 88,
  }];
  result.summary.packages_used = 1;
  return result;
}

async function selectPackingSKUs(page: Page) {
  await expect(page.getByRole("button", { name: /PACK-A/ })).toBeVisible();
  await page.getByRole("button", { name: /PACK-A/ }).click();
  await page.getByRole("button", { name: /PACK-B/ }).click();
  await page.locator(".packing-selected-row", { hasText: "PACK-A" }).getByTitle("增加数量").click();
}

async function expectCanvasPixels(page: Page) {
  const canvas = page.locator('canvas[data-renderer="webgl2"]');
  await expect(canvas).toBeVisible();
  const stats = await canvas.evaluate((node) => {
    const element = node as HTMLCanvasElement;
    const context = element.getContext("webgl2");
    if (!context) return { width: element.clientWidth, height: element.clientHeight, opaque: 0, colors: 0 };
    const pixels = new Uint8Array(element.width * element.height * 4);
    context.readPixels(0, 0, element.width, element.height, context.RGBA, context.UNSIGNED_BYTE, pixels);
    const colors = new Set<string>();
    let opaque = 0;
    const pixelCount = pixels.length / 4;
    const stride = Math.max(1, Math.floor(pixelCount / 8000));
    for (let pixel = 0; pixel < pixelCount; pixel += stride) {
      const offset = pixel * 4;
      if (pixels[offset + 3] > 0) opaque += 1;
      colors.add(`${pixels[offset] >> 4}:${pixels[offset + 1] >> 4}:${pixels[offset + 2] >> 4}:${pixels[offset + 3] >> 4}`);
    }
    return { width: element.clientWidth, height: element.clientHeight, opaque, colors: colors.size };
  });
  expect(stats.width).toBeGreaterThan(250);
  expect(stats.height).toBeGreaterThan(300);
  expect(stats.opaque).toBeGreaterThan(100);
  expect(stats.colors).toBeGreaterThan(4);
}

async function canvasFingerprint(page: Page) {
  return page.locator('canvas[data-renderer="webgl2"]').evaluate((node) => {
    const element = node as HTMLCanvasElement;
    const context = element.getContext("webgl2");
    if (!context) return 0;
    const pixels = new Uint8Array(element.width * element.height * 4);
    context.readPixels(0, 0, element.width, element.height, context.RGBA, context.UNSIGNED_BYTE, pixels);
    let hash = 2166136261;
    const stride = Math.max(4, Math.floor(pixels.length / 32000 / 4) * 4);
    for (let offset = 0; offset < pixels.length; offset += stride) {
      hash = Math.imul(hash ^ pixels[offset], 16777619);
      hash = Math.imul(hash ^ pixels[offset + 1], 16777619);
      hash = Math.imul(hash ^ pixels[offset + 2], 16777619);
      hash = Math.imul(hash ^ pixels[offset + 3], 16777619);
    }
    return hash >>> 0;
  });
}

test("desktop planner requests only SKU quantities and renders calculated packages", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await mockPackingAPI(page);
  await page.goto("./packing");
  await expect(page.getByRole("heading", { name: "包装规划" })).toBeVisible();
  await expect(page.getByText("纸箱参数")).toHaveCount(0);
  await expect(page.getByLabel("纸箱长度")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "2D", exact: true })).toHaveCount(0);
  await expect(page.locator(".warehouse-select")).toHaveCount(0);
  await selectPackingSKUs(page);

  const requestPromise = page.waitForRequest((request) => request.method() === "POST" && request.url().endsWith("/packing/plans"));
  await page.getByRole("button", { name: "生成包装方案" }).click();
  const request = await requestPromise;
  expect(request.postDataJSON()).toEqual({
    items: [{ warehouse_sku: "PACK-A", quantity: 2 }, { warehouse_sku: "PACK-B", quantity: 1 }],
  });

  await expect(page.getByText("3 / 3")).toBeVisible();
  await expect(page.getByRole("tab", { name: "包裹 2 1 件" })).toBeVisible();
  await expect(page.getByRole("button", { name: "保存为组合" })).toBeEnabled();
  await expect(page.locator(".packing-package-spec")).toContainText("32 × 10 × 10 cm");
  await page.getByRole("tab", { name: "包裹 2 1 件" }).click();
  await expect(page.locator(".packing-package-spec")).toContainText("20 × 10 × 10 cm");
  await page.getByRole("tab", { name: "包裹 1 2 件" }).click();
  await expect(page.getByText("旋转方向")).toHaveCount(0);
  await expect(page.getByTestId("packing-scene")).toHaveAttribute("data-active-renderer", "webgl2");
  await page.waitForTimeout(100);
  const beforePlacement = await canvasFingerprint(page);
  await page.getByTitle("下一步").click();
  await expect(page.locator(".packing-step-detail > header")).toContainText("1 / 2");
  await page.waitForTimeout(100);
  expect(await canvasFingerprint(page)).not.toBe(beforePlacement);
  await expectCanvasPixels(page);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await page.screenshot({ path: "/tmp/xlwms-packing-desktop.png", fullPage: true });

  const canvas = page.locator('canvas[data-renderer="webgl2"]');
  const bounds = await canvas.boundingBox();
  expect(bounds).not.toBeNull();
  const beforeOrbit = await canvasFingerprint(page);
  await page.mouse.move(bounds!.x + bounds!.width * 0.5, bounds!.y + bounds!.height * 0.5);
  await page.mouse.down();
  await page.mouse.move(bounds!.x + bounds!.width * 0.64, bounds!.y + bounds!.height * 0.55, { steps: 5 });
  await page.mouse.up();
  await page.waitForTimeout(100);
  expect(await canvasFingerprint(page)).not.toBe(beforeOrbit);
});

test("mobile planner keeps calculated package results usable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await mockPackingAPI(page);
  await page.goto("./packing");
  await selectPackingSKUs(page);
  await page.getByRole("button", { name: "生成包装方案" }).click();
  await expect(page.getByTestId("packing-scene")).toHaveAttribute("data-active-renderer", "webgl2");
  await expect(page.getByRole("button", { name: "2D", exact: true })).toHaveCount(0);
  await page.getByTitle("下一步").click();
  await expectCanvasPixels(page);
  await page.screenshot({ path: "/tmp/xlwms-packing-mobile-3d.png" });

  const layout = await page.evaluate(() => {
    const scene = document.querySelector(".packing-scene")?.getBoundingClientRect();
    const detail = document.querySelector(".packing-step-detail")?.getBoundingClientRect();
    return {
      scrollWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
      sceneRight: scene?.right ?? 0,
      detailTop: detail?.top ?? 0,
      sceneBottom: scene?.bottom ?? 0,
    };
  });
  expect(layout.scrollWidth).toBeLessThanOrEqual(layout.viewportWidth);
  expect(layout.sceneRight).toBeLessThanOrEqual(layout.viewportWidth + 1);
  expect(layout.detailTop).toBeGreaterThanOrEqual(layout.sceneBottom - 1);
  await page.screenshot({ path: "/tmp/xlwms-packing-mobile.png", fullPage: true });
});

test("planner reports unavailable instead of falling back to 2D", async ({ page }) => {
  await page.addInitScript(() => {
    const original = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (contextId: string, ...args: unknown[]) {
      if (contextId === "webgl2") return null;
      return original.call(this, contextId, ...args as []) as RenderingContext | null;
    } as typeof HTMLCanvasElement.prototype.getContext;
  });
  await mockPackingAPI(page);
  await page.goto("./packing");
  await page.getByRole("button", { name: /PACK-A/ }).click();
  await page.getByRole("button", { name: "生成包装方案" }).click();

  await expect(page.getByTestId("packing-scene")).toHaveAttribute("data-active-renderer", "unavailable");
  await expect(page.getByText("三维视图不可用")).toBeVisible();
  await expect(page.locator('canvas[data-renderer="canvas2d"]')).toHaveCount(0);
});

test("combination can be saved with corrections, retrieved, edited, and overwritten", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  const writes: Array<{ method: string; payload: Record<string, unknown> }> = [];
  await mockPackingAPI(page, singlePackagePlan, { onSave: (method, payload) => writes.push({ method, payload }) });
  await page.goto("./packing");
  await selectPackingSKUs(page);
  await page.getByRole("button", { name: "生成包装方案" }).click();

  await page.getByRole("button", { name: "保存为组合" }).click();
  const dialog = page.getByRole("dialog", { name: "保存组合" });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel("组合名称").fill("A 与 B 发货组合");
  await dialog.getByLabel("被替代的发货 SKU").fill("PACK-20PCS");
  await dialog.getByLabel("重量 (kg)").fill("3.05");
  await dialog.getByRole("button", { name: "保存组合", exact: true }).click();
  await expect(page.getByText("组合“A 与 B 发货组合”已保存")).toBeVisible();

  expect(writes).toHaveLength(1);
  expect(writes[0].method).toBe("POST");
  expect(writes[0].payload).toMatchObject({
    name: "A 与 B 发货组合", substitute_for_sku: "PACK-20PCS",
    length_cm: 52, width_cm: 10, height_cm: 10, weight_kg: 3.05,
    calculated_length_cm: 52, calculated_width_cm: 10, calculated_height_cm: 10, calculated_weight_kg: 3.2,
    items: [{ warehouse_sku: "PACK-A", quantity: 2 }, { warehouse_sku: "PACK-B", quantity: 1 }],
  });

  await page.getByRole("tab", { name: "组合库" }).click();
  await expect(page.getByText("A 与 B 发货组合")).toBeVisible();
  await expect(page.getByText("PACK-20PCS")).toBeVisible();
  await expect(page.getByText("已修正")).toBeVisible();
  await page.screenshot({ path: "/tmp/xlwms-packing-combination-library.png", fullPage: true });
  await page.getByRole("button", { name: "编辑组合" }).click();
  await expect(page.getByText("正在修改")).toBeVisible();

  const row = page.locator(".packing-selected-row", { hasText: "PACK-B" });
  await row.getByTitle("增加数量").click();
  await page.getByRole("button", { name: "生成包装方案" }).click();
  await page.getByRole("button", { name: "保存组合修改" }).click();
  const editDialog = page.getByRole("dialog", { name: "修改组合" });
  await editDialog.getByLabel("长度 (cm)").fill("50");
  await editDialog.getByRole("button", { name: "保存修改" }).click();

  expect(writes).toHaveLength(2);
  expect(writes[1].method).toBe("PUT");
  expect(writes[1].payload).toMatchObject({
    name: "A 与 B 发货组合", substitute_for_sku: "PACK-20PCS", length_cm: 50, weight_kg: 3.05,
    items: [{ warehouse_sku: "PACK-A", quantity: 2 }, { warehouse_sku: "PACK-B", quantity: 2 }],
  });
  await expect(page.getByText("组合“A 与 B 发货组合”已保存")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await page.screenshot({ path: "/tmp/xlwms-packing-combination.png", fullPage: true });
});

test("mobile combination library keeps mappings and corrections readable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockPackingAPI(page, packingPlan, { combinations: [{
    id: 51, name: "20pcs 替代组合", substitute_for_sku: "PACK-20PCS",
    length_cm: 22, width_cm: 10, height_cm: 10, weight_kg: 2.3,
    calculated_length_cm: 24, calculated_width_cm: 10, calculated_height_cm: 10, calculated_weight_kg: 2.4,
    corrected: true, note: "移动端检查", enabled: true,
    created_at: "2026-08-26T08:00:00Z", updated_at: "2026-08-26T09:00:00Z",
    items: [
      { warehouse_sku: "PACK-A", product_name: "折叠收纳盒", quantity: 1, length_cm: 20, width_cm: 10, height_cm: 10, weight_kg: 1.2 },
      { warehouse_sku: "PACK-B", product_name: "桌面配件包", quantity: 2, length_cm: 12, width_cm: 8, height_cm: 6, weight_kg: 0.8 },
    ],
  }] });
  await page.goto("./packing");
  await page.getByRole("tab", { name: "组合库" }).click();
  await expect(page.getByText("20pcs 替代组合")).toBeVisible();
  await expect(page.getByText("PACK-20PCS")).toBeVisible();
  await expect(page.getByText("22 × 10 × 10 cm")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await page.screenshot({ path: "/tmp/xlwms-packing-combination-mobile.png", fullPage: true });
});
