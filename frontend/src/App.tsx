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
const PlatformOrdersPage = lazy(() => import("./pages/PlatformOrdersPage"));
const ProductPairingsPage = lazy(() => import("./pages/ProductPairingsPage"));
const SKUSpecsPage = lazy(() => import("./pages/SKUSpecsPage"));
const PackingPlannerPage = lazy(() => import("./pages/PackingPlannerPage"));
const InventoryThresholdsPage = lazy(() => import("./pages/InventoryThresholdsPage"));
const InventoryAlertsPage = lazy(() => import("./pages/InventoryAlertsPage"));
const FulfillmentAuditsPage = lazy(() => import("./pages/FulfillmentAuditsPage"));
const FulfilledOrdersPage = lazy(() => import("./pages/FulfilledOrdersPage"));
const DeliveryEvaluationPage = lazy(() => import("./pages/DeliveryEvaluationPage"));
const WarehousesPage = lazy(() => import("./pages/WarehousesPage"));
const SyncPage = lazy(() => import("./pages/SyncPage"));
const SettingsPage = lazy(() => import("./pages/SettingsPage"));
const validPaths = new Set(["/", "/inventory", "/outbound", "/platform-orders", "/product-pairings", "/fulfillment-audits", "/fulfilled-orders", "/delivery-evaluation", "/costs", "/warehouses", "/inventory-alerts", "/sku-specs", "/packing", "/inventory-thresholds", "/sync", "/settings"]);

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
  else if (path === "/platform-orders") page = <PlatformOrdersPage />;
  else if (path === "/product-pairings") page = <ProductPairingsPage />;
  else if (path === "/fulfillment-audits") page = <FulfillmentAuditsPage warehouse={warehouse} warehouses={warehouses} onWarehouseChange={selectWarehouse} />;
  else if (path === "/fulfilled-orders") page = <FulfilledOrdersPage warehouse={warehouse} warehouses={warehouses} onWarehouseChange={selectWarehouse} />;
  else if (path === "/delivery-evaluation") page = <DeliveryEvaluationPage />;
  else if (path === "/costs") page = <CostsPage warehouse={warehouse} warehouses={warehouses} onWarehouseChange={selectWarehouse} />;
  else if (path === "/inventory-alerts") page = <InventoryAlertsPage warehouse={warehouse} />;
  else if (path === "/sku-specs") page = <SKUSpecsPage />;
  else if (path === "/packing") page = <PackingPlannerPage />;
  else if (path === "/inventory-thresholds") page = <InventoryThresholdsPage />;
  else if (path === "/warehouses") page = <WarehousesPage warehouses={warehouses} onChanged={loadWarehouses} />;
  else if (path === "/sync") page = <SyncPage warehouse={warehouse} warehouses={warehouses} />;
  else if (path === "/settings") page = <SettingsPage online={online} />;

  return (
    <Layout warehouses={warehouses} warehouse={warehouse} onWarehouseChange={selectWarehouse} online={online} path={path} onNavigate={navigate}>
      <Suspense fallback={<LoadingState label="正在加载工作区" />}>{page}</Suspense>
    </Layout>
  );
}
