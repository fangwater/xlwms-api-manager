package store

import "testing"

func TestValidateOMSLoginRequiresUsernameAndPassword(t *testing.T) {
	if err := validateOMSLogin("", "secret"); err == nil {
		t.Fatal("empty username must fail")
	}
	if err := validateOMSLogin("operator", ""); err == nil {
		t.Fatal("empty password must fail")
	}
	if err := validateOMSLogin("operator", "secret"); err != nil {
		t.Fatal(err)
	}
}
