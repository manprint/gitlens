package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// pagerDutyEndpoint is the Events API v2 address; tests point it at httptest.
var pagerDutyEndpoint = "https://events.pagerduty.com/v2/enqueue"

// PagerDutyHost is what the API shows of a PagerDuty channel (I-7).
const PagerDutyHost = "events.pagerduty.com"

const pagerDutySummaryMax = 1024

var pagerDutyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)

// ErrPagerDutyKey is the refusal of a routing key; it never quotes the key.
var ErrPagerDutyKey = errors.New("routing key must be 32 letters or digits")

// ValidatePagerDutyKey checks an Events API v2 routing (integration) key.
func ValidatePagerDutyKey(key string) error {
	if !pagerDutyKeyPattern.MatchString(key) {
		return ErrPagerDutyKey
	}
	return nil
}

type pagerDuty struct {
	key    string
	client *http.Client
}

// NewPagerDuty returns a channel that sends Events API v2 events with the
// routing key of a PagerDuty service.
func NewPagerDuty(routingKey string, hc *http.Client) Channel {
	return &pagerDuty{key: routingKey, client: hc}
}

func (p *pagerDuty) Name() string { return "pagerduty" }

func (p *pagerDuty) Send(ctx context.Context, m Message) error {
	b, err := json.Marshal(pagerDutyEvent(p.key, m))
	if err != nil {
		return err
	}
	if err := sendJSON(ctx, p.client, pagerDutyEndpoint, b); err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), p.key, "[redacted key]"))
	}
	return nil
}

// pagerDutyEvent is the R1 event: a resolve carries only the key, the action
// and the dedup key; anything else triggers.
func pagerDutyEvent(key string, m Message) map[string]any {
	if m.Phase == "resolve" {
		return map[string]any{"routing_key": key, "event_action": "resolve", "dedup_key": m.DedupID}
	}
	source := m.Instance
	if source == "" {
		source = m.Cluster
	}
	if source == "" {
		source = "pglens"
	}
	details := map[string]any{"value": m.Value}
	if m.Database != "" {
		details["database"] = m.Database
	}
	for k, v := range m.Labels {
		if _, taken := details[k]; !taken {
			details[k] = v
		}
	}
	if m.Detail != nil {
		if lines := pagerDutyDetailLines(m.Detail); len(lines) > 0 {
			details["detail"] = lines
		}
	}
	payload := map[string]any{
		"summary":        clip(m.Summary, pagerDutySummaryMax),
		"source":         source,
		"severity":       pagerDutySeverity(m.Severity),
		"component":      "postgresql",
		"custom_details": details,
	}
	if !m.At.IsZero() {
		payload["timestamp"] = m.At.UTC().Format(time.RFC3339)
	}
	if m.Cluster != "" {
		payload["group"] = m.Cluster
	}
	if m.RuleID != "" {
		payload["class"] = m.RuleID
	}
	event := map[string]any{"routing_key": key, "event_action": "trigger", "payload": payload}
	if m.DedupID != "" {
		event["dedup_key"] = m.DedupID
	}
	return event
}

func pagerDutySeverity(s string) string {
	switch s {
	case "critical", "warning":
		return s
	default:
		return "info"
	}
}

// pagerDutyDetailLines is the detail as plain lines: title, fields, sections
// and footer.
func pagerDutyDetailLines(d *Detail) []string {
	var lines []string
	if d.Title != "" {
		lines = append(lines, d.Title)
	}
	for _, f := range d.Fields {
		lines = append(lines, f.Label+": "+f.Value)
	}
	for _, s := range d.Sections {
		if s.Title != "" {
			lines = append(lines, s.Title)
		}
		lines = append(lines, s.Lines...)
		lines = append(lines, s.Code...)
	}
	if d.Footer != "" {
		lines = append(lines, d.Footer)
	}
	return lines
}
