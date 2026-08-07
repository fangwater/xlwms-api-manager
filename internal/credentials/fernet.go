package credentials

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fernet/fernet-go"
)

type Cipher struct {
	key *fernet.Key
}

func EnsureKeyFile(path string) (*Cipher, error) {
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create credential key directory: %w", err)
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		var generated fernet.Key
		if err := generated.Generate(); err != nil {
			return nil, fmt.Errorf("generate credential key: %w", err)
		}
		if err := os.WriteFile(path, []byte(generated.Encode()+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("write credential key: %w", err)
		}
		raw = []byte(generated.Encode())
	} else if err != nil {
		return nil, fmt.Errorf("read credential key: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("restrict credential key permissions: %w", err)
	}
	key, err := fernet.DecodeKey(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("invalid credential key file: %w", err)
	}
	return &Cipher{key: key}, nil
}

func (c *Cipher) Encrypt(value string) (string, error) {
	token, err := fernet.EncryptAndSign([]byte(value), c.key)
	if err != nil {
		return "", fmt.Errorf("encrypt credential: %w", err)
	}
	return string(token), nil
}

func (c *Cipher) Decrypt(token string) (string, error) {
	value := fernet.VerifyAndDecrypt([]byte(token), 0*time.Second, []*fernet.Key{c.key})
	if value == nil {
		return "", errors.New("cannot decrypt warehouse credentials")
	}
	return string(value), nil
}

func MaskAppKey(appKey string) string {
	runes := []rune(appKey)
	if len(runes) <= 8 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:4]) + "..." + string(runes[len(runes)-4:])
}

func MaskIdentifier(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) == 0 {
		return ""
	}
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:2]) + "***" + string(runes[len(runes)-2:])
}
