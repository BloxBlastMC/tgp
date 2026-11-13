package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("hash = %s", h)
	}
	if err := VerifyPassword("correct horse battery", h); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword("wrong", h); err != ErrPasswordMismatch {
		t.Fatalf("want mismatch, got %v", err)
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("hashes should be salted")
	}
}

func TestValidatePassword(t *testing.T) {
	for _, bad := range []string{"", "short", "          ", strings.Repeat("x", 300)} {
		if ValidatePassword(bad) == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	if err := ValidatePassword("long enough pw"); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedCredentials(t *testing.T) {
	tp := NewTempPassword()
	if len(tp) != 19 || strings.Count(tp, "-") != 3 {
		t.Fatalf("temp password %q", tp)
	}
	if ValidatePassword(tp) != nil {
		t.Fatal("temp password must satisfy policy")
	}
	pat := NewPAT()
	if !strings.HasPrefix(pat, PATPrefix) || len(pat) < 40 {
		t.Fatalf("pat %q", pat)
	}
	rc := NewRecoveryCode()
	if NormalizeRecoveryCode(strings.ToUpper(rc)) != strings.ReplaceAll(rc, "-", "") {
		t.Fatal("recovery code normalisation")
	}
	if HashToken("a") == HashToken("b") || len(HashToken("a")) != 64 {
		t.Fatal("hash token")
	}
}

func TestTOTPReplayProtection(t *testing.T) {
	secret, url, err := NewTOTPKey("tgp", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "otpauth://totp/") {
		t.Fatal(url)
	}
	now := time.Unix(1_800_000_000, 0)
	code, _ := TOTPCode(secret, now)
	step, ok := VerifyTOTP(secret, code, 0, now)
	if !ok {
		t.Fatal("valid code rejected")
	}
	if _, ok := VerifyTOTP(secret, code, step, now); ok {
		t.Fatal("replayed code accepted")
	}
	// one step of skew is tolerated
	if _, ok := VerifyTOTP(secret, code, 0, now.Add(30*time.Second)); !ok {
		t.Fatal("skewed code rejected")
	}
	if _, ok := VerifyTOTP(secret, code, 0, now.Add(5*time.Minute)); ok {
		t.Fatal("stale code accepted")
	}
	if _, ok := VerifyTOTP(secret, "12345", 0, now); ok {
		t.Fatal("short code accepted")
	}
}

func TestFailureLimiter(t *testing.T) {
	l := NewFailureLimiter(3, time.Minute)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if b, _ := l.Blocked("k"); b {
			t.Fatalf("blocked after %d", i)
		}
		l.Fail("k")
	}
	if b, _ := l.Blocked("k"); !b {
		t.Fatal("should be blocked")
	}
	now = now.Add(2 * time.Minute)
	if b, _ := l.Blocked("k"); b {
		t.Fatal("should unblock after window")
	}
	l.Fail("k")
	l.Reset("k")
	if b, _ := l.Blocked("k"); b {
		t.Fatal("reset")
	}
}
