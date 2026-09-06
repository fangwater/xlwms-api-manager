import { defineConfig } from "@playwright/test";

const port = process.env.XLWMS_E2E_PORT ?? "5174";
const baseURL = process.env.XLWMS_E2E_BASE_URL ?? `http://127.0.0.1:${port}/warehouse-console/`;

export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  reporter: "line",
  use: {
    baseURL,
    colorScheme: "light",
    locale: "zh-CN",
    trace: "retain-on-failure"
  },
  webServer: process.env.XLWMS_E2E_BASE_URL ? undefined : {
    command: `npm run dev -- --host 127.0.0.1 --port ${port}`,
    url: baseURL,
    reuseExistingServer: true,
    timeout: 120_000
  }
});
