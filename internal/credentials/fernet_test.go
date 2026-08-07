package credentials

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeyFileAndCipherArePrivateAndReusable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warehouse.key")
	cipher, err := EnsureKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	token, err := cipher.Encrypt("app-secret")
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := EnsureKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := reloaded.Decrypt(token)
	if err != nil || plain != "app-secret" {
		t.Fatalf("decrypt returned %q, %v", plain, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode is %o", info.Mode().Perm())
	}
}

func TestMaskAppKey(t *testing.T) {
	if got := MaskAppKey("1234567890abcdef"); got != "1234...cdef" {
		t.Fatalf("unexpected mask %q", got)
	}
	if got := MaskAppKey("short"); got != "*****" {
		t.Fatalf("unexpected short mask %q", got)
	}
}

func TestMaskIdentifier(t *testing.T) {
	if got := MaskIdentifier("shipping@example.com"); got != "sh***om" {
		t.Fatalf("unexpected mask %q", got)
	}
	if got := MaskIdentifier("user"); got != "****" {
		t.Fatalf("unexpected short mask %q", got)
	}
}
