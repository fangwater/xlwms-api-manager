package temu

// Public binding metadata contains identifiers only, never authentication material.
type WarehouseAPIBinding struct {
	CredentialKey string `json:"credential_key"`
	OMSAccountKey string `json:"oms_account_key"`
}
