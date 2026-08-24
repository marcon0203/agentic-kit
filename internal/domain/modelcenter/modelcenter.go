// Package modelcenter is the 模型中心 bounded context (spec-09):
// registering the model-provider credentials a run will use, and
// reporting the usage those runs generate.
//
// The two belong together because they are the two halves of one
// question — which providers may this user reach, and what has reaching
// them cost. Both are scoped to a single user, and one rule governs each:
// a credential is proven to work before it is stored and never leaves
// again, and usage belongs to whoever triggered the run.
package modelcenter

import "time"

// Provider is one registered model-provider credential. There is no field
// for the credential itself: this type is what read paths return, and the
// only way to keep a secret out of a response reliably is for the value
// never to be on the type that gets serialised.
type Provider struct {
	ID        int64
	OwnerID   int64
	Name      string
	BaseURL   string
	Status    int16
	CreatedAt time.Time
}

// KnownProviders are the provider names the platform can validate against.
// DeepSeek and Qwen (via DashScope's OpenAI-compatible mode) are both
// OpenAI-wire-compatible, like "custom", which is accepted so a
// self-hosted, OpenAI-compatible endpoint can be registered too.
var KnownProviders = []string{"anthropic", "openai", "google", "deepseek", "qwen", "custom"}

// Period is the window a usage report covers.
type Period string

const (
	PeriodDay   Period = "day"
	PeriodWeek  Period = "week"
	PeriodMonth Period = "month"
)

func ParsePeriod(s string) (Period, bool) {
	switch Period(s) {
	case PeriodDay, PeriodWeek, PeriodMonth:
		return Period(s), true
	case "":
		return PeriodMonth, true
	default:
		return "", false
	}
}

// GroupBy is how a usage report is broken down.
type GroupBy string

const (
	GroupByBundle GroupBy = "bundle"
	GroupByDay    GroupBy = "day"
)

func ParseGroupBy(s string) (GroupBy, bool) {
	switch GroupBy(s) {
	case GroupByBundle, GroupByDay:
		return GroupBy(s), true
	case "":
		return GroupByBundle, true
	default:
		return "", false
	}
}

// Window is the period a report covers: everything since Since, labelled
// as Label.
type Window struct {
	Since time.Time
	Label string
}

// WindowFor computes the period boundary in UTC. Periods start at a
// boundary rather than counting back N days, so "this month" means the
// calendar month a user would check their bill against.
func WindowFor(now time.Time, period Period) Window {
	now = now.UTC()
	switch period {
	case PeriodDay:
		since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return Window{Since: since, Label: since.Format("2006-01-02")}
	case PeriodWeek:
		// Monday-anchored, matching the ISO week convention. Go's Sunday=0
		// needs the shift; without it a Sunday would start its own week.
		offset := (int(now.Weekday()) + 6) % 7
		since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -offset)
		return Window{Since: since, Label: since.Format("2006-01-02")}
	default:
		since := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return Window{Since: since, Label: since.Format("2006-01")}
	}
}

// UsageBucket is one row of a usage breakdown — a bundle ref or a day,
// depending on how the report was grouped.
type UsageBucket struct {
	Key      string
	Tokens   int64
	CostUSD  float64
	RunCount int32
}

// UsageReport is the answer to "what have I spent this period".
type UsageReport struct {
	Period       string
	TotalTokens  int64
	TotalCostUSD float64
	RunCount     int32
	Breakdown    []UsageBucket
}
