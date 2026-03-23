// Package auth provides token utilities used by the auth commands.
package auth

import "strings"

// MaskToken returns a partially masked token for display.
// "tok_abcdef123456" → "tok_***...***456"
// Tokens shorter than 8 chars are fully masked.
func MaskToken(token string) string {
	if len(token) < 8 {
		return strings.Repeat("*", len(token))
	}
	suffix := token[len(token)-4:]
	return token[:3] + "_***...***" + suffix
}
