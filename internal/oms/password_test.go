package oms

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUpgradeRequiredPasswordUsesSecuritySessionAndVerifiesLogin(t *testing.T) {
	const (
		currentPassword = "Old!Password7Q"
		newPassword     = "Fresh!Moon9Qz"
	)
	loginCount := 0
	upgradeCount := 0
	serverURL := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Track-Key"); got == "" || !strings.HasPrefix(got, "v2:") {
			t.Errorf("missing Track-Key: %q", got)
		}
		switch request.URL.Path {
		case "/gateway/woms/auth/login":
			loginCount++
			var payload loginPayload
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
				return
			}
			switch payload.Password {
			case currentPassword:
				writeOMSJSON(writer, apiEnvelope[loginData]{
					Code: 4011,
					Data: loginData{LoginAction: "NEED_UPDATE_PASSWORD", SecuritySessionToken: "session-token", NeedForceUpdatePassword: true},
					Msg:  "请更新登录密码",
				})
			case newPassword:
				writeOMSJSON(writer, apiEnvelope[loginData]{Code: http.StatusOK, Data: loginData{Token: "fresh-token"}, Msg: "ok"})
			default:
				writeOMSJSON(writer, apiEnvelope[loginData]{Code: http.StatusUnauthorized, Msg: "invalid login"})
			}
		case passwordUpgradePath:
			upgradeCount++
			if request.Header.Get("Referer") != serverURL+"/login" {
				t.Errorf("Referer = %q", request.Header.Get("Referer"))
			}
			var payload passwordUpgradePayload
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
				return
			}
			if payload.SecuritySessionToken != "session-token" || payload.NewPassword != newPassword || payload.ConfirmPassword != newPassword {
				t.Errorf("unexpected password upgrade payload")
			}
			writeOMSJSON(writer, apiEnvelope[any]{Code: http.StatusOK, Msg: "ok"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client := NewClient(server.URL, "demo-user", currentPassword, time.Second)
	if err := client.UpgradeRequiredPassword(context.Background(), newPassword); err != nil {
		t.Fatal(err)
	}
	if err := client.CheckAccess(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loginCount != 2 || upgradeCount != 1 {
		t.Fatalf("login count = %d, upgrade count = %d", loginCount, upgradeCount)
	}
}

func TestUpgradeRequiredPasswordRejectsLoginWithoutUpgrade(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeOMSJSON(writer, apiEnvelope[loginData]{Code: http.StatusOK, Data: loginData{Token: "test-token"}})
	}))
	defer server.Close()

	client := NewClient(server.URL, "demo-user", "Old!Password7Q", time.Second)
	err := client.UpgradeRequiredPassword(context.Background(), "Fresh!Moon9Qz")
	if !errors.Is(err, ErrPasswordUpdateNotRequired) {
		t.Fatalf("error = %v, want ErrPasswordUpdateNotRequired", err)
	}
}

func TestValidateNewPassword(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		password string
		valid    bool
	}{
		{name: "valid", current: "Old!Password7Q", password: "Fresh!Moon9Qz", valid: true},
		{name: "same", current: "Same!Password7Q", password: "Same!Password7Q"},
		{name: "too short", current: "Old!Password7Q", password: "Tiny!9Aa"},
		{name: "missing special", current: "Old!Password7Q", password: "FreshMoon9Qz"},
		{name: "ascending sequence", current: "Old!Password7Q", password: "Abcdef!Moon9Q"},
		{name: "descending sequence", current: "Old!Password7Q", password: "Zyxwvu!Moon9Q"},
		{name: "digit sequence", current: "Old!Password7Q", password: "Fresh!123456Qz"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateNewPassword(test.current, test.password)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidNewPassword) {
				t.Fatalf("error = %v, want ErrInvalidNewPassword", err)
			}
		})
	}
}
