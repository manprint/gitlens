package server

import (
	"fmt"
	"net/url"
	"strings"
)

// traceIDLabel is the stored form of a trace_id label: 32 lowercase hex
// characters, not all zeros; anything else stores NULL.
func traceIDLabel(v string) *string {
	if !validTraceID(v) {
		return nil
	}
	return &v
}

const traceIDRule = "trace_id must be 32 lowercase hexadecimal characters, not all zeros"

func validTraceID(v string) bool {
	if len(v) != 32 || v == strings.Repeat("0", 32) {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validateTraceURLTemplate checks PGLENS_TRACE_URL_TEMPLATE: empty, or an
// http(s) URL with a host that contains {trace_id} at least once.
func validateTraceURLTemplate(tpl string) error {
	if tpl == "" {
		return nil
	}
	if !strings.Contains(tpl, "{trace_id}") {
		return fmt.Errorf("PGLENS_TRACE_URL_TEMPLATE must contain {trace_id}")
	}
	u, err := url.Parse(strings.ReplaceAll(tpl, "{trace_id}", "0"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("PGLENS_TRACE_URL_TEMPLATE must be an http:// or https:// URL")
	}
	return nil
}
