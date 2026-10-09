package fulfillment

import "strings"

// Keys remain stable for existing orders; geography is an optional filter.
type Warehouse struct {
	Key                 string   `json:"warehouse_key"`
	Code                string   `json:"wh_code"`
	Name                string   `json:"display_name"`
	Provider            string   `json:"provider"`
	Region              string   `json:"region"`
	Priority            int      `json:"inventory_priority"`
	FulfillmentEnabled  bool     `json:"fulfillment_enabled"`
	InventoryConnected  bool     `json:"inventory_connected"`
	InventoryRegistered bool     `json:"inventory_registered"`
	Revision            int64    `json:"revision"`
	EnabledShopCount    int      `json:"enabled_shop_count"`
	Enabled             bool     `json:"enabled"`
	AllowedCarrierCodes []string `json:"allowed_carrier_codes"`
}

var Warehouses = []Warehouse{
	{Key: "DPS002", Code: "DPSNY002", Name: "DPS-纽约", Provider: "DPS", Region: "east", Priority: 1},
	{Key: "ARP_EAST", Code: "HYTX30", Name: "ARP-宾夕法尼亚", Provider: "ARP", Region: "east", Priority: 2},
	{Key: "DPS004", Code: "DPSCA004", Name: "DPS-加州", Provider: "DPS", Region: "west", Priority: 1},
	{Key: "ARP_WEST", Code: "ARPCA01", Name: "ARP-洛杉矶", Provider: "ARP", Region: "west", Priority: 2},
	{Key: "ARP_HOUSTON", Code: "ARP06A", Name: "ARP-休斯顿", Provider: "ARP", Region: "central", Priority: 2},
	{Key: "ARP_ATLANTA", Code: "ARPGA", Name: "ARP-亚特兰大", Provider: "ARP", Region: "east", Priority: 2},
}

func Keys() []string {
	keys := make([]string, 0, len(Warehouses))
	for _, w := range Warehouses {
		keys = append(keys, w.Key)
	}
	return keys
}

func Find(identifier string) (Warehouse, bool) {
	identifier = strings.ToUpper(strings.TrimSpace(identifier))
	for _, w := range Warehouses {
		if w.Key == identifier || w.Code == identifier {
			return w, true
		}
	}
	return Warehouse{}, false
}

// The collection warehouses cannot accept any other carrier, including an
// unknown carrier or an OMS label-upload mode such as _AUTO_MATCH_.
func CarrierAllowed(identifier, code string) bool {
	w, ok := Find(identifier)
	if !ok || (w.Key != "ARP_HOUSTON" && w.Key != "ARP_ATLANTA") {
		return true
	}
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "USPS", "GOFO", "UPS", "FEDEX", "SPEEDX", "CBS":
		return true
	default:
		return false
	}
}

func CarrierCode(values ...string) string {
	text := strings.ToUpper(strings.Join(values, " "))
	text = strings.NewReplacer(" ", "", "-", "", "_", "", "/", "").Replace(text)
	matched := ""
	for _, code := range []string{"UNIUNI", "SWIFTX", "SPEEDX", "YANWEN", "FEDEX", "USPS", "UPS", "GOFO", "CBS"} {
		if strings.Contains(text, code) {
			if matched != "" && matched != code {
				return ""
			}
			matched = code
		}
	}
	return matched
}
