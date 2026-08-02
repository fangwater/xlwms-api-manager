import { Suspense, lazy, useCallback, useEffect, useState } from "react";
import { api } from "./api";
import { LoadingState } from "./components/Common";
import Layout from "./components/Layout";
import { useRouter } from "./router";
import type { Warehouse } from "./types";

const DashboardPage = lazy(() => import("./pages/DashboardPage"));
const InventoryPage = lazy(() => import("./pages/InventoryPage"));
const CostsPage = lazy(() => import("./pages/CostsPage"));
const OutboundPage = lazy(() => import("./pages/OutboundPage"));
const WarehousesPage = lazy(() => import("./pages/WarehousesPage"));
const SyncPage = lazy(() => import("./pages/SyncPage"));
const SettingsPage = lazy(() => import("./pages/SettingsPage"));
const validPaths = new Set(["/", "/inventory", "/outbound", "/costs", "/warehouses", "/sync", "/settings"]);

export default function App() {
  const { path, navigate } = useRouter();
  const [warehouses, setWarehouses] = useState<Warehouse[]>([]);
  const [warehouse, setWarehouse] = useState(() => localStorage.getItem("xlwms-warehouse") || "");
  const [online, setOnline] = useState<boolean | null>(null);
  const loadWarehouses = useCallback(async () => {
    try {
      setWarehouses(await api.warehouses());
    } catch {
      setWarehouses([]);
    }
  }, []);

  useEffect(() => {
    void api.health().then(() => setOnline(true)).catch(() => setOnline(false));
    void loadWarehouses();
  }, [loadWarehouses]);
  useEffect(() => {
    if (!validPaths.has(path)) navigate("/");
  }, [path]);

  const selectWarehouse = (value: string) => {
    setWarehouse(value);
    if (value) localStorage.setItem("xlwms-warehouse", value);
    else localStorage.removeItem("xlwms-warehouse");
  };
  let page = <DashboardPage warehouse={warehouse} warehouses={warehouses} />;
  if (path === "/inventory") page = <InventoryPage warehouse={warehouse} warehouses={warehouses} />;
  else if (path === "/outbound") page = <OutboundPage warehouse={warehouse} />;
  else if (path === "/costs") page = <CostsPage warehouse={warehouse} />;
  else if (path === "/warehouses") page = <WarehousesPage warehouses={warehouses} onChanged={loadWarehouses} />;
  else if (path === "/sync") page = <SyncPage warehouse={warehouse} warehouses={warehouses} />;
  else if (path === "/settings") page = <SettingsPage online={online} />;

  return (
    <Layout warehouses={warehouses} warehouse={warehouse} onWarehouseChange={selectWarehouse} online={online} path={path} onNavigate={navigate}>
      <Suspense fallback={<LoadingState label="正在加载工作区" />}>{page}</Suspense>
    </Layout>
  );
}
