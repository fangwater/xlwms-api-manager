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

function packingPlan(carton: Record<string, number>) {
  return {
    algorithm: "bp3d-pivot-v1",
    heuristic: true,
    carton,
    cartons: [
      {
        index: 1,
        placements: [
          {
            step: 1, unit_id: "PACK-A#1", warehouse_sku: "PACK-A", product_name: "折叠收纳盒",
            position: { x: 0, y: 0, z: 0 }, dimensions: { length_cm: 20, width_cm: 10, height_cm: 10 },
            original_dimensions: { length_cm: 20, width_cm: 10, height_cm: 10 }, weight_kg: 1.2, rotation: "WHD",
          },
          {
            step: 2, unit_id: "PACK-B#1", warehouse_sku: "PACK-B", product_name: "桌面配件包",
            position: { x: 20, y: 0, z: 0 }, dimensions: { length_cm: 12, width_cm: 8, height_cm: 6 },
            original_dimensions: { length_cm: 12, width_cm: 8, height_cm: 6 }, weight_kg: 0.8, rotation: "WHD",
          },
        ],
        packed_units: 2, used_weight_kg: 2, used_volume_cm3: 2576, volume_utilization_percent: 8.587,
      },
      {
        index: 2,
        placements: [{
          step: 1, unit_id: "PACK-A#2", warehouse_sku: "PACK-A", product_name: "折叠收纳盒",
          position: { x: 0, y: 0, z: 0 }, dimensions: { length_cm: 20, width_cm: 10, height_cm: 10 },
          original_dimensions: { length_cm: 20, width_cm: 10, height_cm: 10 }, weight_kg: 1.2, rotation: "WHD",
        }],
        packed_units: 1, used_weight_kg: 1.2, used_volume_cm3: 2000, volume_utilization_percent: 6.667,
      },
    ],
    unfit_items: [],
    summary: {
      requested_units: 3, packed_units: 3, unfit_units: 0, cartons_used: 2, cartons_available: carton.count,
      total_weight_kg: 3.2, packed_weight_kg: 3.2, packed_volume_cm3: 4576,
    },
  };
}

async function mockPackingAPI(page: Page) {
  await page.route("**/warehouse-console/healthz", (route) => route.fulfill({
    status: 200, contentType: "application/json", body: JSON.stringify({ success: true, data: { status: "ok" } }),
  }));
  await page.route("**/warehouse-console/api/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    let data: unknown = {};
    if (path.endsWith("/warehouses")) data = [];
    else if (path.endsWith("/warehouse-sku-specs")) data = { records: skuSpecs, total: 2, page: 1, page_size: 30, pages: 1 };
    else if (path.endsWith("/packing/plans")) {
      const payload = route.request().postDataJSON() as { carton: Record<string, number> };
      data = packingPlan(payload.carton);
    }
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ success: true, data }) });
  });
}

async function selectPackingSKUs(page: Page) {
  await expect(page.getByRole("button", { name: /PACK-A/ })).toBeVisible();
  await page.getByRole("button", { name: /PACK-A/ }).click();
  await page.getByRole("button", { name: /PACK-B/ }).click();
  await page.locator(".packing-selected-row", { hasText: "PACK-A" }).getByTitle("增加数量").click();
}

async function expectCanvasPixels(page: Page, expectedRenderer: "webgl2" | "canvas2d") {
  const canvas = page.locator(`canvas[data-renderer="${expectedRenderer}"]`);
  await expect(canvas).toBeVisible();
  const stats = await canvas.evaluate((node) => {
    const element = node as HTMLCanvasElement;
    let pixels: Uint8Array | Uint8ClampedArray;
    if (element.dataset.renderer === "webgl2") {
      const context = element.getContext("webgl2");
      if (!context) return { width: element.clientWidth, height: element.clientHeight, opaque: 0, colors: 0 };
      pixels = new Uint8Array(element.width * element.height * 4);
      context.readPixels(0, 0, element.width, element.height, context.RGBA, context.UNSIGNED_BYTE, pixels);
    } else {
      const context = element.getContext("2d");
      if (!context) return { width: element.clientWidth, height: element.clientHeight, opaque: 0, colors: 0 };
      pixels = context.getImageData(0, 0, element.width, element.height).data;
    }
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

test("desktop packing planner renders animated WebGL and Canvas fallback", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await mockPackingAPI(page);
  await page.goto("./packing");
  await expect(page.getByRole("heading", { name: "装箱规划" })).toBeVisible();
  await expect(page.locator(".warehouse-select")).toHaveCount(0);
  await selectPackingSKUs(page);

  const requestPromise = page.waitForRequest((request) => request.method() === "POST" && request.url().endsWith("/packing/plans"));
  await page.getByRole("button", { name: "生成装箱方案" }).click();
  const request = await requestPromise;
  expect(request.postDataJSON()).toEqual({
    items: [{ warehouse_sku: "PACK-A", quantity: 2 }, { warehouse_sku: "PACK-B", quantity: 1 }],
    carton: { length_cm: 40, width_cm: 30, height_cm: 25, max_weight_kg: 20, count: 2 },
  });

  await expect(page.getByText("3 / 3")).toBeVisible();
  await expect(page.getByRole("tab", { name: "箱 2 1 件" })).toBeVisible();
  await page.getByTitle("下一步").click();
  await expect(page.locator(".packing-step-detail > header")).toContainText("1 / 2");
  const renderer = await page.getByTestId("packing-scene").getAttribute("data-active-renderer");
  expect(renderer).toBe("webgl2");
  await page.waitForTimeout(500);
  await expectCanvasPixels(page, "webgl2");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
  await page.screenshot({ path: "/tmp/xlwms-packing-desktop.png", fullPage: true });

  await page.getByRole("button", { name: "2D", exact: true }).click();
  await expect(page.getByTestId("packing-scene")).toHaveAttribute("data-active-renderer", "canvas2d");
  await expectCanvasPixels(page, "canvas2d");
});

test("mobile packing planner stacks controls and keeps 2D fallback usable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await mockPackingAPI(page);
  await page.goto("./packing");
  await selectPackingSKUs(page);
  await page.getByRole("button", { name: "生成装箱方案" }).click();
  await page.getByRole("button", { name: "2D", exact: true }).click();
  await page.getByTitle("下一步").click();
  await expect(page.getByTestId("packing-scene")).toHaveAttribute("data-active-renderer", "canvas2d");
  await expectCanvasPixels(page, "canvas2d");

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
