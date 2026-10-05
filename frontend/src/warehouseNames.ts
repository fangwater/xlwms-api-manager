const arpWarehouseNames: Record<string, string> = {
  HYTX30: "ARP-宾夕法尼亚",
  ARP_EAST: "ARP-宾夕法尼亚",
  ARPCA01: "ARP-洛杉矶",
  ARP_WEST: "ARP-洛杉矶",
  ARP06A: "ARP-休斯顿",
  ARP_HOUSTON: "ARP-休斯顿",
  ARPGA: "ARP-亚特兰大",
  ARP_ATLANTA: "ARP-亚特兰大"
};

export function warehouseDisplayName(code?: string, fallbackName?: string): string {
  const identifier = code?.trim() || "";
  return arpWarehouseNames[identifier.toUpperCase()] || fallbackName?.trim() || identifier || "-";
}

export function warehouseCarrierAllowed(warehouseKey: string,carrierCode: string): boolean {
  if (!["ARP_HOUSTON","ARP06A","ARP_ATLANTA","ARPGA"].includes(warehouseKey.toUpperCase())) return true;
  return ["USPS","GOFO","UPS","FEDEX"].includes(carrierCode.toUpperCase());
}
