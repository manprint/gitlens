package redact

import "unicode/utf8"

// IdentityMaxBytes bounds a free-text name that a monitored database supplies
// and that is shipped to name a session, a standby or a prepared transaction:
// an application_name or a prepared transaction's gid.
const IdentityMaxBytes = 64

// Identity returns the form in which a free-text name from a monitored database
// is shipped: the name cut to IdentityMaxBytes at a character boundary, or
// Withheld when the name may hold a credential. The whole name is checked as
// well as the cut one, so a credential sign that the cut would hide is still
// withheld.
func Identity(name string) string {
	if MayHoldCredential(name) {
		return Withheld
	}
	cut := truncateUTF8(name, IdentityMaxBytes)
	if MayHoldCredential(cut) {
		return Withheld
	}
	return cut
}

// truncateUTF8 returns the longest prefix of s that is at most max bytes long
// and ends on a character boundary.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
