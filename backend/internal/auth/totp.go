package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	totpPeriod = 30
	totpDigits = 6
	// totpSkew allows one time step either side of the current one to absorb
	// client/server clock drift.
	totpSkew = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret returns a fresh random base32 secret (160-bit) suitable
// for use as a TOTP key.
func GenerateTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return b32.EncodeToString(buf), nil
}

// TOTPCode computes the 6-digit code valid for the given time.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	counter := uint64(t.Unix()) / totpPeriod
	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg)
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	code := bin % uint32(pow10(totpDigits))
	return fmt.Sprintf("%0*d", totpDigits, code), nil
}

// ValidateTOTP reports whether code matches the secret within the allowed
// clock-skew window. Comparison is constant-time.
func ValidateTOTP(secret, code string) bool {
	if secret == "" || code == "" {
		return false
	}
	now := time.Now()
	for i := -totpSkew; i <= totpSkew; i++ {
		want, err := TOTPCode(secret, now.Add(time.Duration(i*totpPeriod)*time.Second))
		if err != nil {
			return false
		}
		if hmac.Equal([]byte(want), []byte(strings.TrimSpace(code))) {
			return true
		}
	}
	return false
}

// OTPAuthURI builds the otpauth:// provisioning URI consumed by authenticator
// apps (and QR-code generators).
func OTPAuthURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(totpDigits))
	q.Set("period", strconv.Itoa(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

func pow10(n int) uint32 {
	var v uint32 = 1
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}
