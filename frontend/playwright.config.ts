import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  reporter: "line",
  use: {
    baseURL: "http://127.0.0.1:5174/warehouse-console/",
    colorScheme: "light",
    locale: "zh-CN",
    trace: "retain-on-failure"
  },
  webServer: {
    command: "npm run dev -- --host 127.0.0.1",
    url: "http://127.0.0.1:5174/warehouse-console/",
    reuseExistingServer: true,
    timeout: 120_000
  }
});
