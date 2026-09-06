package store

import (
	"errors"
	"testing"
)

func TestValidateOMSAccountIdentity(t *testing.T) {
	key, label, err := validateOMSAccountIdentity(" Backup-Account ", " 备用账户 ")
	if err != nil {
		t.Fatal(err)
	}
	if key != "backup-account" || label != "备用账户" {
		t.Fatalf("normalized account = %q %q", key, label)
	}
	for _, invalid := range []string{"", "-backup", "bad account", "账户"} {
		if _, _, err := validateOMSAccountIdentity(invalid, "备用账户"); !errors.Is(err, ErrInvalidFulfillmentAccount) {
			t.Fatalf("key %q error = %v", invalid, err)
		}
	}
	if _, _, err := validateOMSAccountIdentity("backup", " "); !errors.Is(err, ErrInvalidFulfillmentAccount) {
		t.Fatalf("empty label error = %v", err)
	}
}

func TestNormalizeAPICredentialKeysAllowsOneAccountForMultipleAPIs(t *testing.T) {
	first, err := normalizeAPICredentialKeys([]string{" api-one ", "api-two", "api-one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0] != "api-one" || first[1] != "api-two" {
		t.Fatalf("API credential normalization = %#v", first)
	}
	if _, err := normalizeAPICredentialKeys([]string{""}); !errors.Is(err, ErrInvalidFulfillmentAccount) {
		t.Fatalf("empty API credential error = %v", err)
	}
}
