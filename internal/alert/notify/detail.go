package notify

import (
	"strings"
	"unicode/utf8"
)

// Detail is what a notification says beyond its one-line summary: the
// problem, where it is, the data that shows it and what to do, already in the
// reader's language. A message without one renders as before.
type Detail struct {
	Title    string    `json:"title"`
	Fields   []Field   `json:"fields,omitempty"`
	Sections []Section `json:"sections,omitempty"`
	Footer   string    `json:"footer,omitempty"`
	Locale   string    `json:"locale,omitempty"`
}

// Field is one labelled fact: the cluster, the server, the measured value.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Section is a block of evidence or advice: plain lines, then statements to
// run, shown as code.
type Section struct {
	Title string   `json:"title"`
	Lines []string `json:"lines,omitempty"`
	Code  []string `json:"code,omitempty"`
}

// Slack caps a header at 150 characters and a section's text at 3000.
const (
	slackHeaderMax  = 150
	slackSectionMax = 3000
	slackBlocksMax  = 50
)

func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// clip cuts s to max runes, ending with "…" when it cut, and closes a code
// fence the cut left open.
func clip(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	out := string(r[:max-1]) + "…"
	if strings.Count(out, "```")%2 == 1 {
		out = string(r[:max-5]) + "…\n```"
	}
	return out
}

func codeBlock(lines []string) string {
	return "```" + strings.Join(lines, "\n") + "```"
}

func slackBlocks(m Message) []any {
	d := m.Detail
	blocks := []any{map[string]any{"type": "header", "text": map[string]any{"type": "plain_text", "text": clip(d.Title, slackHeaderMax), "emoji": true}}}
	if len(d.Fields) > 0 {
		lines := make([]string, 0, len(d.Fields))
		for _, f := range d.Fields {
			lines = append(lines, "*"+slackEscape(f.Label)+":* "+slackEscape(f.Value))
		}
		blocks = append(blocks, mrkdwnSection(strings.Join(lines, "\n")))
	}
	for _, s := range d.Sections {
		if len(blocks) >= slackBlocksMax-1 {
			break
		}
		var b strings.Builder
		if s.Title != "" {
			b.WriteString("*" + slackEscape(s.Title) + "*")
		}
		for _, l := range s.Lines {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(slackEscape(l))
		}
		if len(s.Code) > 0 {
			esc := make([]string, len(s.Code))
			for i, c := range s.Code {
				esc[i] = slackEscape(c)
			}
			b.WriteString("\n" + codeBlock(esc))
		}
		blocks = append(blocks, map[string]any{"type": "divider"}, mrkdwnSection(b.String()))
	}
	if d.Footer != "" {
		blocks = append(blocks, map[string]any{"type": "context", "elements": []any{map[string]string{"type": "mrkdwn", "text": slackEscape(d.Footer)}}})
	}
	if len(blocks) > slackBlocksMax {
		blocks = blocks[:slackBlocksMax]
	}
	return blocks
}

func mrkdwnSection(text string) map[string]any {
	return map[string]any{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": clip(text, slackSectionMax)}}
}

// detailText renders a detail as Google Chat text: *bold* labels and ```
// fenced statements.
func detailText(d *Detail) string {
	var b strings.Builder
	b.WriteString("*" + d.Title + "*")
	for _, f := range d.Fields {
		b.WriteString("\n*" + f.Label + ":* " + f.Value)
	}
	for _, s := range d.Sections {
		b.WriteString("\n")
		if s.Title != "" {
			b.WriteString("\n*" + s.Title + "*")
		}
		for _, l := range s.Lines {
			b.WriteString("\n" + l)
		}
		if len(s.Code) > 0 {
			b.WriteString("\n" + codeBlock(s.Code))
		}
	}
	if d.Footer != "" {
		b.WriteString("\n\n_" + d.Footer + "_")
	}
	return b.String()
}
