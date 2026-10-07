import { expect, test, type Page } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

async function setup(page: Page) {
  const root = resolve("dist");
  const warehouses = [
    {warehouse_key:"ARP_HOUSTON",wh_code:"ARP06A",display_name:"ARP-休斯顿",provider:"ARP",region:"central",enabled:false,fulfillment_enabled:false,inventory_connected:true,revision:1,enabled_shop_count:0,allowed_carrier_codes:["USPS","GOFO","UPS","FEDEX"]},
    {warehouse_key:"ARP_ATLANTA",wh_code:"ARPGA",display_name:"ARP-亚特兰大",provider:"ARP",region:"east",enabled:false,fulfillment_enabled:false,inventory_connected:false,revision:1,enabled_shop_count:0,allowed_carrier_codes:["USPS","GOFO","UPS","FEDEX"]}
  ];
  const bindings = [
    {platform:"temu",shop_code:"panda-homes",shop_name:"Panda Homes"},
    {platform:"temu",shop_code:"panda-buy",shop_name:"Panda Buy"},
    {platform:"shein",shop_code:"beauty-hangers-home",shop_name:"Beauty Hangers home"}
  ].map(b=>({...b,warehouse_key:"ARP_HOUSTON",oms_code:"ARP06A",shop_enabled:true,platform_warehouse_id:"",platform_warehouse_name:"",enabled:false,effective:false,revision:0,warehouse_revision:1,verified_at:null as string|null,verification:{},updated_at:null}));
  const control = {ready:false,conflict:false,platformFails:false,mutations:[] as {path:string; body:Record<string,unknown>}[]};
  await page.route("**/warehouse-console/**",async route=>{
    const path=new URL(route.request().url()).pathname;
    const relative=path.includes("/assets/")?path.slice(path.indexOf("/assets/")+1):"index.html";
    const contentType=relative.endsWith(".js")?"application/javascript":relative.endsWith(".css")?"text/css":"text/html";
    await route.fulfill({body:await readFile(resolve(root,relative)),contentType});
  });
  await page.route("**/warehouse-console/api/**",async route=>{
    const request=route.request();const path=new URL(request.url()).pathname.replace("/warehouse-console/api","");
    const body=request.postDataJSON() || {};
    const reply=async(data:unknown,status=200,error?:string)=>route.fulfill({status,json:error?{success:false,error}:{success:true,data}});
    if(request.method()!=="GET") {
      control.mutations.push({path,body});
      if(!request.headers().authorization)return reply(null,401,"管理认证失败");
    }
    if(path==="/warehouses")return reply([]);
    if(path==="/fulfillment-warehouses")return reply(warehouses);
    if(path==="/warehouse-api-credentials")return reply([{key:"test-api",label:"ARP 衣架",warehouse_codes:["ARP06A"],oms_account_label:"ARP 衣架账号",sku_count:0,inventory_sync_status:"ready",active:true,last_verified_at:"2026-10-07T00:00:00Z",updated_at:"2026-10-07T00:00:00Z",app_key_hint:"hint",deletable:false}]);
    if(path.endsWith("/platform-bindings"))return reply(bindings);
    if(path.endsWith("/history"))return reply([]);
    if(path.endsWith("/platform-options/query"))return control.platformFails?reply(null,409,"需有效订单验证平台仓"):reply([{id:"WH-SHEIN-HOUSTON",name:"ARP Houston",can_ship:true,oms_code:"ARP06A"}]);
    if(path.includes("/platform-bindings/")&&request.method()==="PUT") {
      if(control.conflict)return reply(null,409,"配置已变化，请刷新页面后重试");
      const b=bindings.find(b=>path.endsWith(`/${b.platform}/${b.shop_code}`))!;
      b.platform_warehouse_id=body.platform_warehouse_id;b.platform_warehouse_name=body.platform_warehouse_name;b.enabled=false;b.effective=false;b.revision++;
      return reply(b);
    }
    if(path.endsWith("/readiness")||path.endsWith("/activation")) {
      const b=bindings.find(b=>b.platform===body.platform&&b.shop_code===body.shop_code)!;
      const checks=[{code:"inventory",label:"库存查询",passed:true,message:"接口可查询，库存为零不阻止配置"},{code:"oms_permission",label:"OMS 发货权限",passed:control.ready,message:control.ready?"账号有权限":"账号尚未授权该仓"}];
      const result={warehouse_key:"ARP_HOUSTON",platform:b.platform,shop_code:b.shop_code,revision:b.revision,warehouse_revision:b.warehouse_revision,ready:control.ready,checks,checked_at:"2026-10-07T00:00:00Z"};
      if(path.endsWith("/activation")) {
        if(!control.ready)return reply(null,409,"检查未通过");
        b.enabled=true;b.effective=true;b.revision++;b.verified_at=result.checked_at;warehouses[0].fulfillment_enabled=true;warehouses[0].enabled=true;warehouses[0].enabled_shop_count=1;warehouses[0].revision++;
        for(const item of bindings)item.warehouse_revision=warehouses[0].revision;
      }
      return reply(result);
    }
    return reply({});
  });
  await page.route("**/warehouse-console/healthz",route=>route.fulfill({json:{success:true,data:{status:"ok"}}}));
  await page.goto("https://pangutech.online/warehouse-console/warehouses");
  await page.getByRole("row").filter({hasText:"ARP-休斯顿"}).getByRole("button",{name:"配置与检查"}).click();
  await page.getByRole("textbox",{name:"管理用户名"}).fill("test-console");
  await page.getByRole("textbox",{name:"管理密码"}).fill("test-password");
  await page.getByRole("button",{name:"SHEIN",exact:true}).click();
  await expect(page.getByRole("combobox",{name:"平台店铺"})).toHaveValue("beauty-hangers-home");
  return {control,bindings,warehouses};
}

test("SHEIN draft, checks, activation and pause remain isolated to the selected shop",async({page})=>{
  const {control,bindings}=await setup(page);
  await page.getByRole("button",{name:"同步平台仓库"}).click();
  await page.getByRole("combobox",{name:"选择平台仓库"}).selectOption("WH-SHEIN-HOUSTON");
  await expect(page.getByRole("button",{name:"启用仓库和此店铺"})).toBeDisabled();
  await page.getByRole("button",{name:"保存草稿"}).click();
  await page.getByRole("button",{name:"检查配置",exact:true}).click();
  await expect(page.getByLabel("配置检查结果")).toContainText("账号尚未授权该仓");
  await expect(page.getByRole("button",{name:"启用仓库和此店铺"})).toBeDisabled();
  control.ready=true;
  await page.getByRole("button",{name:"检查配置",exact:true}).click();
  await expect(page.getByRole("button",{name:"启用仓库和此店铺"})).toBeEnabled();
  await page.getByRole("button",{name:"启用仓库和此店铺"}).click();
  await expect(page.getByRole("button",{name:"暂停此店铺"})).toBeVisible();
  expect(bindings.filter(b=>b.platform==="temu").every(b=>!b.enabled)).toBe(true);
  await page.getByRole("button",{name:"暂停此店铺"}).click();
  await expect(page.getByRole("button",{name:"暂停此店铺"})).not.toBeVisible();
  expect(bindings[2].platform_warehouse_id).toBe("WH-SHEIN-HOUSTON");
  expect(control.mutations.some(m=>m.path.endsWith("/activation")&&m.body.shop_code==="beauty-hangers-home")).toBe(true);
});

test("stale page cannot overwrite mapping and reports refresh action",async({page})=>{
  const {control}=await setup(page);control.conflict=true;
  await page.getByRole("textbox",{name:"平台仓 ID",exact:true}).fill("WH-SHEIN-HOUSTON");
  await page.getByRole("button",{name:"保存草稿"}).click();
  await expect(page.getByRole("dialog")).toContainText("配置已变化，请刷新页面后重试");
  await expect(page.getByRole("button",{name:"启用仓库和此店铺"})).toBeDisabled();
});

test("missing SHEIN order still allows a disabled draft",async({page})=>{
  const {control,bindings}=await setup(page);control.platformFails=true;
  await page.getByRole("button",{name:"同步平台仓库"}).click();
  await expect(page.getByRole("dialog")).toContainText("需有效订单验证平台仓");
  await page.getByRole("textbox",{name:"平台仓 ID",exact:true}).fill("WH-SHEIN-HOUSTON");
  await page.getByRole("button",{name:"保存草稿"}).click();
  await expect(page.getByRole("dialog")).toContainText("配置已保存");
  expect(bindings[2].enabled).toBe(false);
});
