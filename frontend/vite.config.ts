import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const apiTarget = process.env.XLWMS_DEV_API_TARGET ?? "http://127.0.0.1:18083";

export default defineConfig({
  base: "/warehouse-console/",
  plugins: [react()],
  server: {
    host: "0.0.0.0",
    port: 5174,
    strictPort: true,
    proxy: {
      "/warehouse-console/healthz": {
        target: apiTarget,
        rewrite: () => "/healthz"
      },
      "/warehouse-console/evaluation-api": {
        target: "http://127.0.0.1:18087",
        rewrite: (path) => path.replace(/^\/warehouse-console\/evaluation-api/, "/v1")
      },
      "/warehouse-console/api": {
        target: apiTarget,
        rewrite: (path) => path.replace(/^\/warehouse-console\/api/, "/v1")
      }
    }
  }
});
