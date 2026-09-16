package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPRoundTrip(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	if len(secret) != 32 { // 20 bytes -> 32 base32 chars, no padding
		t.Fatalf("unexpected secret length %d", len(secret))
	}
	now := time.Now()
	code, err := TOTPCode(secret, now)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if len(code) != 6 || strings.TrimLeft(code, "0123456789") != "" {
		t.Fatalf("code %q is not 6 digits", code)
	}
	if !ValidateTOTP(secret, code) {
		t.Fatalf("ValidateTOTP rejected its own current code %q", code)
	}
}

func TestTOTPSkew(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	// A code from one step in the past must still validate (clock drift).
	past, _ := TOTPCode(secret, time.Now().Add(-30*time.Second))
	if !ValidateTOTP(secret, past) {
		t.Fatalf("expected previous-step code %q to validate within skew", past)
	}
	// A code far outside the window must not.
	far, _ := TOTPCode(secret, time.Now().Add(-5*time.Minute))
	if ValidateTOTP(secret, far) {
		t.Fatalf("expected stale code %q to be rejected", far)
	}
}

func TestTOTPInvalidInputs(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	if ValidateTOTP(secret, "") {
		t.Fatal("empty code must not validate")
	}
	if ValidateTOTP("", "123456") {
		t.Fatal("empty secret must not validate")
	}
	if ValidateTOTP(secret, "000000") && ValidateTOTP(secret, "999999") {
		t.Fatal("implausible that both fixed codes validate")
	}
}

func TestOTPAuthURI(t *testing.T) {
	uri := OTPAuthURI("Gerrit Go", "alice", "JBSWY3DPEHPK3PXP")
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("bad scheme/host: %q", uri)
	}
	for _, want := range []string{"secret=JBSWY3DPEHPK3PXP", "issuer=Gerrit", "algorithm=SHA1", "digits=6", "period=30"} {
		if !strings.Contains(uri, want) {
			t.Fatalf("uri %q missing %q", uri, want)
		}
	}
}
