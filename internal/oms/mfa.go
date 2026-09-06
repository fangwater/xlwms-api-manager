package oms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	mfaSendCodePath = "/gateway/woms/auth/sendVerifyCode"
	mfaVerifyPath   = "/gateway/woms/auth/verifyCodeLogin"
)

var (
	ErrMFAVerificationRequired = errors.New("OMS MFA verification required")
	ErrMFANotRequired          = errors.New("OMS MFA verification is not required")
	ErrMFAChallengeNotStarted  = errors.New("OMS MFA challenge is not active")
	ErrInvalidMFACode          = errors.New("invalid OMS MFA verification code")
)

// Keep only diagnostic codes, never upstream messages or authentication data.
type MFARequestError struct {
	Stage            string
	HTTPStatus       int
	Code             int
	AttemptsExceeded bool
}

func (e *MFARequestError) Error() string {
	return fmt.Sprintf("OMS MFA %s failed (HTTP %d, code %d)", e.Stage, e.HTTPStatus, e.Code)
}

func (e *MFARequestError) PublicMessage() string {
	if e.Stage == "verify" {
		switch {
		case e.Code == 4013:
			return "验证码会话已失效，请关闭窗口并重新发起验证"
		case e.Code == 4012 && e.AttemptsExceeded:
			return "验证码尝试次数已用完，请关闭窗口并重新发起验证"
		case e.Code == 4012:
			return "验证码校验未通过，请使用本次验证对应的最新验证码"
		}
	}
	stage := "发送验证码"
	if e.Stage == "verify" {
		stage = "校验验证码"
	}
	return fmt.Sprintf("领星%s失败（HTTP %d，错误码 %d）", stage, e.HTTPStatus, e.Code)
}

type MFAPrompt struct {
	Channel      string `json:"channel"`
	MaskedTarget string `json:"masked_target,omitempty"`
	CodeSent     bool   `json:"code_sent"`
	CodeLength   int    `json:"code_length"`
}

type mfaTarget struct {
	Channel      string `json:"channel"`
	MaskedTarget string `json:"maskedTarget"`
}

type mfaLoginSession struct {
	loginFlowID       string
	deviceFingerprint string
	deviceInfo        string
}

type mfaVerificationSession struct {
	login       mfaLoginSession
	challengeID string
	prompt      MFAPrompt
}

type mfaVerificationRequiredError struct {
	session mfaLoginSession
	data    loginData
}

func (e *mfaVerificationRequiredError) Error() string { return ErrMFAVerificationRequired.Error() }
func (e *mfaVerificationRequiredError) Unwrap() error { return ErrMFAVerificationRequired }

type mfaSendCodePayload struct {
	ChallengeID string `json:"challengeId"`
	Channel     string `json:"channel"`
	Language    string `json:"language"`
}

type mfaVerifyPayload struct {
	ChallengeID string `json:"challengeId"`
	VerifyCode  string `json:"verifyCode"`
}

func newLoginSession() (mfaLoginSession, error) {
	flowID, err := randomHex(16)
	if err != nil {
		return mfaLoginSession{}, fmt.Errorf("generate OMS login flow ID: %w", err)
	}
	return mfaLoginSession{
		loginFlowID: flowID, deviceFingerprint: deviceFingerprint(), deviceInfo: deviceInfo(),
	}, nil
}

func (c *Client) BeginMFAVerification(ctx context.Context) (MFAPrompt, error) {
	c.mfaMu.Lock()
	defer c.mfaMu.Unlock()
	c.tokenMu.Lock()
	hasToken := c.token != ""
	c.tokenMu.Unlock()
	if hasToken {
		return MFAPrompt{}, ErrMFANotRequired
	}
	c.mfa = nil

	token, err := c.login(ctx)
	if err == nil {
		c.tokenMu.Lock()
		c.token = token
		c.tokenMu.Unlock()
		c.mfa = nil
		return MFAPrompt{}, ErrMFANotRequired
	}
	var required *mfaVerificationRequiredError
	if !errors.As(err, &required) {
		return MFAPrompt{}, err
	}

	channel := strings.TrimSpace(required.data.MFAChannel)
	maskedTarget := strings.TrimSpace(required.data.MFAMaskedTarget)
	if channel == "" && len(required.data.AvailableTargets) > 0 {
		channel = strings.TrimSpace(required.data.AvailableTargets[0].Channel)
		maskedTarget = strings.TrimSpace(required.data.AvailableTargets[0].MaskedTarget)
	}
	challengeID := strings.TrimSpace(required.data.ChallengeID)
	if challengeID == "" || channel == "" {
		return MFAPrompt{}, errors.New("OMS did not return an MFA challenge")
	}
	prompt := MFAPrompt{Channel: channel, MaskedTarget: maskedTarget, CodeLength: 6}
	// The official login page also calls sendVerifyCode for TOTP to activate
	// the challenge, even though no SMS or email is sent for that channel.
	envelope, status, requestErr := postJSON[any](ctx, c, mfaSendCodePath, mfaSendCodePayload{
		ChallengeID: challengeID, Channel: channel, Language: "zh",
	}, nil)
	if requestErr != nil {
		return MFAPrompt{}, fmt.Errorf("send OMS MFA verification code: %w", requestErr)
	}
	if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
		return MFAPrompt{}, &MFARequestError{Stage: "send", HTTPStatus: status, Code: envelope.Code}
	}
	prompt.CodeSent = !strings.EqualFold(channel, "TOTP")
	c.mfa = &mfaVerificationSession{login: required.session, challengeID: challengeID, prompt: prompt}
	return prompt, nil
}

func (c *Client) CompleteMFAVerification(ctx context.Context, code string) error {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return ErrInvalidMFACode
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return ErrInvalidMFACode
		}
	}

	c.mfaMu.Lock()
	defer c.mfaMu.Unlock()
	if c.mfa == nil {
		return ErrMFAChallengeNotStarted
	}
	session := c.mfa
	envelope, status, err := postJSON[struct {
		AttemptsExceeded bool `json:"attemptsExceeded"`
	}](ctx, c, mfaVerifyPath, mfaVerifyPayload{
		ChallengeID: session.challengeID, VerifyCode: code,
	}, nil)
	if err != nil {
		return fmt.Errorf("verify OMS MFA code: %w", err)
	}
	if status < 200 || status >= 300 || envelope.Code != http.StatusOK {
		if envelope.Code == 4013 || envelope.Code == 4012 && envelope.Data.AttemptsExceeded {
			c.mfa = nil
		}
		return &MFARequestError{Stage: "verify", HTTPStatus: status, Code: envelope.Code, AttemptsExceeded: envelope.Data.AttemptsExceeded}
	}
	token, err := c.loginWithSession(ctx, session.login)
	if err != nil {
		return fmt.Errorf("complete OMS MFA login: %w", err)
	}
	c.tokenMu.Lock()
	c.token = token
	c.tokenMu.Unlock()
	c.mfa = nil
	return nil
}
