package sample

import (
	"errors"
	"regexp"
	"strings"
)

// emailRegex is a coarse pattern: local@domain with at least one dot in
// the domain. Real validation is a much harder problem (RFC 5322), but
// the fixture only needs a recognizable shape so retrieval tests work.
var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// ValidateEmail returns nil if the address looks structurally valid.
// Empty or malformed input returns a descriptive error so callers can
// surface the failure reason to the user.
func ValidateEmail(addr string) error {
	if addr == "" {
		return errors.New("validator: empty email address")
	}
	if !emailRegex.MatchString(addr) {
		return errors.New("validator: invalid email format")
	}
	return nil
}

// NormalizeEmail lowercases the address and trims surrounding whitespace
// so two visually identical addresses compare equal. Used before storing
// the email in a case-insensitive index.
func NormalizeEmail(addr string) string {
	return strings.ToLower(strings.TrimSpace(addr))
}

// CheckPasswordStrength reports whether the password meets the baseline
// length and character-class requirements. Returns the reason string on
// failure ("too short", "no digit", "no upper") so the caller can show
// a precise error to the user.
func CheckPasswordStrength(pw string) (ok bool, reason string) {
	if len(pw) < 8 {
		return false, "too short"
	}
	hasDigit := false
	hasUpper := false
	for _, r := range pw {
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
		if r >= 'A' && r <= 'Z' {
			hasUpper = true
		}
	}
	if !hasDigit {
		return false, "no digit"
	}
	if !hasUpper {
		return false, "no upper"
	}
	return true, ""
}

// SanitizeUsername strips leading and trailing whitespace and rejects
// names that contain control characters. Returns the cleaned form or
// the empty string when the input is unusable.
func SanitizeUsername(name string) string {
	name = strings.TrimSpace(name)
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return name
}
