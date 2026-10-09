import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

const carriers = ["GOFO", "SWIFTX", "SPEEDX", "YANWEN", "UPS", "USPS", "FEDEX", "CBS"];

for (const platform of ["temu", "shein"]) {
  test(`${platform} collection carrier switches save and survive reload`, async ({ page }) => {
    const groups = ["ARP_HOUSTON", "ARP_ATLANTA"].map((warehouseKey) => ({
      warehouse_key: warehouseKey,
      source: "platform_default",
      base_rules: {
        warehouse_key: warehouseKey, allowed_carrier_codes: ["USPS", "GOFO", "UPS", "FEDEX"],
        allow_signature: false, allowed_currency_codes: ["USD"], selection_mode: "lowest_price",
        max_price_delta: 0, warehouse_tie_priority: 2
      },
      carriers: carriers.map((code, index) => ({
        warehouse_key: warehouseKey, carrier_code: code, priority: index + 1,
        enabled: ["USPS", "GOFO", "UPS", "FEDEX"].includes(code)
      }))
    }));
    const changedCarriers = platform === "shein" ? ["SPEEDX", "CBS"] : ["SPEEDX"];
    // Verify the built assets against the public origin without a trial server.
    await page.route("**/warehouse-console/assets/**", async (route) => {
      const name = new URL(route.request().url()).pathname.split("/").at(-1)!;
      await route.fulfill({ body: await readFile(resolve("dist/assets", name)), contentType: name.endsWith(".css") ? "text/css" : "application/javascript" });
    });
    await page.route("**/warehouse-console/shipping-policies/**", async (route) => {
      await route.fulfill({ body: await readFile(resolve("dist/index.html")), contentType: "text/html" });
    });
    await page.route("**/warehouse-console/healthz", (route) => route.fulfill({ json: { status: "ok" } }));
    await page.route("**/warehouse-console/api/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      let data: unknown = [];
      if (path.endsWith("/fulfillment-policies/carriers")) data = groups;
      else if (/\/fulfillment-policies\/carriers\/[^/]+$/.test(path)) {
        const group = groups.find((item) => item.warehouse_key === path.split("/").at(-1))!;
        const payload = route.request().postDataJSON();
        Object.assign(group, { base_rules: payload.base_rules, carriers: payload.carriers });
        data = group;
      }
      await route.fulfill({ json: { success: true, data } });
    });
    await page.goto("./shipping-policies/base-rules");
    if (platform === "shein") await page.getByRole("button", { name: "SHEIN", exact: true }).click();
    for (const name of ["ARP-休斯顿", "ARP-亚特兰大"]) {
      const card = page.locator(".warehouse-policy-card").filter({ hasText: name });
      for (const code of changedCarriers) {
        const checkbox = card.getByRole("checkbox", { name: code, exact: true });
        await expect(checkbox).toBeEnabled();
        await checkbox.locator("..").click();
        await expect(checkbox).toBeChecked();
      }
      await expect(card.getByRole("checkbox", { name: "YANWEN", exact: true })).toBeDisabled();
      await card.getByRole("button", { name: "保存规则" }).click();
      await expect(card.locator(".saved.visible")).toBeVisible();
    }
    await page.reload();
    for (const code of changedCarriers) await expect(page.getByRole("checkbox", { name: code, exact: true }).first()).toBeChecked();
    await page.locator(".policy-view-nav").getByRole("button", { name: "快递选择算法" }).click();
    for (const name of ["ARP-休斯顿", "ARP-亚特兰大"]) {
      const card = page.locator(".warehouse-policy-card").filter({ hasText: name });
      for (const code of changedCarriers) {
        const checkbox = card.getByRole("checkbox", { name: code, exact: true });
        await expect(checkbox).toBeEnabled();
        await checkbox.locator("..").click();
        await expect(checkbox).toBeChecked();
      }
      await card.getByRole("button", { name: "保存规则" }).click();
      await expect(card.locator(".saved.visible")).toBeVisible();
    }
    await page.reload();
    for (const name of ["ARP-休斯顿", "ARP-亚特兰大"]) {
      const card = page.locator(".warehouse-policy-card").filter({ hasText: name });
      for (const code of changedCarriers) await expect(card.getByRole("checkbox", { name: code, exact: true })).toBeChecked();
    }
  });
}
