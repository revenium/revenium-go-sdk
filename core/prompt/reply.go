package prompt

import (
	"strings"
	"unicode/utf8"
)

// redactionMargin is how far past REVENIUM_MAX_PROMPT_SIZE a streamed reply is kept, so a
// credential that crosses the limit is still whole, or at least longer than any shape's
// minimum, when it is redacted; the cut to the limit only happens after redaction.
const redactionMargin = 4096

// StreamedReplyLimit is the most characters of a streamed reply kept for capture.
func StreamedReplyLimit() int {
	return GetMaxPromptSize() + redactionMargin
}

// ReplyBudget is the one bound on what a streamed reply keeps for capture: every part
// written through it counts against StreamedReplyLimit characters, and it records whether
// it had to drop anything.
type ReplyBudget struct {
	held  int
	limit int
	cut   bool
}

func NewReplyBudget() *ReplyBudget {
	return &ReplyBudget{limit: StreamedReplyLimit()}
}

// Write appends as much of text to into as the budget has room for.
func (b *ReplyBudget) Write(into *strings.Builder, text string) {
	if text == "" {
		return
	}
	room := b.limit - b.held
	if utf8.RuneCountInString(text) > room {
		text = string([]rune(text)[:max(room, 0)])
		b.cut = true
	}
	into.WriteString(text)
	b.held += utf8.RuneCountInString(text)
}

// Keep reports whether text fits in what is left and counts it if so; text that does not
// fit is dropped whole and recorded as a cut.
func (b *ReplyBudget) Keep(text string) bool {
	return b.Charge(utf8.RuneCountInString(text))
}

// Charge counts size characters against the budget if they fit, recording a cut if not.
func (b *ReplyBudget) Charge(size int) bool {
	if size > b.limit-b.held {
		b.cut = true
		return false
	}
	b.held += size
	return true
}

// Cut reports whether anything was dropped.
func (b *ReplyBudget) Cut() bool {
	return b.cut
}

// ReplyCollector gathers a streamed reply for prompt capture and reports whether it had to
// drop any of it. A nil collector, used when capture is off, ignores what it is given.
type ReplyCollector struct {
	text   strings.Builder
	budget *ReplyBudget
	ended  bool
}

func NewReplyCollector() *ReplyCollector {
	return &ReplyCollector{budget: NewReplyBudget()}
}

func (c *ReplyCollector) Add(text string) {
	if c != nil {
		c.budget.Write(&c.text, text)
	}
}

func (c *ReplyCollector) Text() string {
	if c == nil {
		return ""
	}
	return c.text.String()
}

// Truncated reports whether part of the reply was dropped.
func (c *ReplyCollector) Truncated() bool {
	return c != nil && c.budget.Cut()
}

// End records that the provider reported the reply finished. A clean transport close does
// not count, so a reply cut off by a dropped connection stays partial and is sanitized as cut.
func (c *ReplyCollector) End() {
	if c != nil {
		c.ended = true
	}
}

// Complete reports whether the whole reply was kept: the stream ended and nothing was dropped.
func (c *ReplyCollector) Complete() bool {
	return c != nil && c.ended && !c.budget.Cut()
}
