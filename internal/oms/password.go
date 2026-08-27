package oms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
)

const passwordUpgradePath = "/gateway/woms/auth/securityUpgrade/updatePassword"

var (
	ErrPasswordUpdateRequired    = errors.New("OMS password update required")
	ErrPasswordUpdateNotRequired = errors.New("OMS password update is not required")
	ErrInvalidNewPassword        = errors.New("invalid new OMS password")
)

type passwordUpdateRequiredError struct {
	securitySessionToken string
}

func (e *passwordUpdateRequiredError) Error() string {
	return ErrPasswordUpdateRequired.Error()
}

func (e *passwordUpdateRequiredError) Unwrap() error {
	return ErrPasswordUpdateRequired
}

type passwordUpgradePayload struct {
	SecuritySessionToken string `json:"securitySessionToken"`
	NewPassword          string `json:"newPassword"`
	ConfirmPassword      string `json:"confirmPassword"`
}

// UpgradeRequiredPassword completes the password-upgrade branch returned by
// the OMS login endpoint, then verifies a fresh login with the new password.
func (c *Client) UpgradeRequiredPassword(ctx context.Context, newPassword string) error {
	if err := validateNewPassword(c.password, newPassword); err != nil {
		return err
	}
	_, err := c.login(ctx)
	if err == nil {
		return ErrPasswordUpdateNotRequired
	}
	var required *passwordUpdateRequiredError
	if !errors.As(err, &required) {
		return err
	}
	if required.securitySessionToken == "" {
		return errors.New("OMS password update session is unavailable")
	}
	payload := passwordUpgradePayload{
		SecuritySessionToken: required.securitySessionToken,
		NewPassword:          newPassword,
		ConfirmPassword:      newPassword,
	}
	envelope, status, requestErr := postJSON[any](ctx, c, passwordUpgradePath, payload, map[string]string{
		"Referer": c.baseURL + "/login",
	})
	if requestErr != nil {
		return fmt.Errorf("update required OMS password: %w", requestErr)
	}
	if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
		return remoteError("OMS required password update", status, envelope.Code, envelope.Msg)
	}

	c.tokenMu.Lock()
	c.password = newPassword
	c.token = ""
	c.tokenMu.Unlock()
	if _, err := c.accessToken(ctx); err != nil {
		return fmt.Errorf("verify OMS login after password update: %w", err)
	}
	return nil
}

func validateNewPassword(currentPassword, newPassword string) error {
	if newPassword == currentPassword {
		return fmt.Errorf("%w: new password must differ from current password", ErrInvalidNewPassword)
	}
	length := len([]rune(newPassword))
	if length < 12 || length > 20 {
		return fmt.Errorf("%w: password must contain 12 to 20 characters", ErrInvalidNewPassword)
	}
	var upper, lower, digit, special bool
	for _, character := range newPassword {
		switch {
		case unicode.IsUpper(character):
			upper = true
		case unicode.IsLower(character):
			lower = true
		case unicode.IsDigit(character):
			digit = true
		default:
			special = true
		}
	}
	if !upper || !lower || !digit || !special {
		return fmt.Errorf("%w: password must contain uppercase, lowercase, numeric, and special characters", ErrInvalidNewPassword)
	}
	if hasSequentialPasswordCharacters(newPassword, 6) {
		return fmt.Errorf("%w: password must not contain six sequential letters or digits", ErrInvalidNewPassword)
	}
	return nil
}

func hasSequentialPasswordCharacters(value string, sequenceLength int) bool {
	characters := []rune(strings.ToLower(value))
	for start := 0; start+sequenceLength <= len(characters); start++ {
		sequence := characters[start : start+sequenceLength]
		allDigits := true
		allLetters := true
		for _, character := range sequence {
			allDigits = allDigits && character >= '0' && character <= '9'
			allLetters = allLetters && character >= 'a' && character <= 'z'
		}
		if !allDigits && !allLetters {
			continue
		}
		difference := sequence[1] - sequence[0]
		if difference != 1 && difference != -1 {
			continue
		}
		sequential := true
		for index := 2; index < len(sequence); index++ {
			if sequence[index]-sequence[index-1] != difference {
				sequential = false
				break
			}
		}
		if sequential {
			return true
		}
	}
	return false
}
