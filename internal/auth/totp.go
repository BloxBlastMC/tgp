package auth

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const totpPeriod = 30

// NewTOTPKey generates a TOTP secret for account (shown as issuer:account
// in authenticator apps).
func NewTOTPKey(issuer, account string) (secret, url string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      totpPeriod,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1, // what every authenticator app supports
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// VerifyTOTP checks a 6-digit code against secret, allowing one step of
// clock skew either way. lastStep is the most recent step already used for
// this secret; codes at or before it are rejected to stop replay. On
// success it returns the step the code matched, to be stored as the new
// lastStep.
func VerifyTOTP(secret, code string, lastStep int64, now time.Time) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0, false
	}
	cur := now.Unix() / totpPeriod
	for _, step := range []int64{cur - 1, cur, cur + 1} {
		if step <= lastStep {
			continue
		}
		want, err := totp.GenerateCodeCustom(secret, time.Unix(step*totpPeriod, 0), totp.ValidateOpts{
			Period:    totpPeriod,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// TOTPCode generates the current code for secret (used in tests and tools).
func TOTPCode(secret string, t time.Time) (string, error) {
	c, err := totp.GenerateCodeCustom(secret, t, totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return "", fmt.Errorf("generate totp: %w", err)
	}
	return c, nil
}
