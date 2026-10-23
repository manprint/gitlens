package check

import "github.com/manprint/pglens/internal/redact"

// withheldQuery stands for a statement text that is not shipped: one that may
// hold a credential (see package redact), or a session text the server cut,
// which may hide the keyword that makes one.
const withheldQuery = redact.Withheld

// redactQueryText withholds a statement text that may hold a credential
// before it is cached or shipped, and returns any other text unchanged.
func redactQueryText(query string) string {
	if redact.MayHoldCredential(query) {
		return withheldQuery
	}
	return query
}
