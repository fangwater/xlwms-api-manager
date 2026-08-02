import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  base: "/warehouse-console/",
  plugins: [react()],
  server: {
    host: "0.0.0.0",
    port: 5174,
    strictPort: true,
    proxy: {
      "/warehouse-console/healthz": {
        target: "http://127.0.0.1:18083",
        rewrite: () => "/healthz"
      },
      "/warehouse-console/api": {
        target: "http://127.0.0.1:18083",
        rewrite: (path) => path.replace(/^\/warehouse-console\/api/, "/v1")
      }
    }
  }
});
