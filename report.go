package onair

import "time"

// Report is the core's whole output and a public API: its JSON shape only
// changes additively. Rendering it is the host's job - the CLI's tty renderer
// and an admin popover are just two consumers.
type Report struct {
	Project     string            `json:"project"`
	Environment string            `json:"environment"`
	Forge       ForgeInfo         `json:"forge"`
	Head        *CommitInfo       `json:"head,omitempty"`
	Green       *GreenInfo        `json:"green,omitempty"`
	Components  []ComponentReport `json:"components"`
	Drift       *Drift            `json:"drift"`
	Attribution AttributionMode   `json:"attribution"`
	// Errors carries degradation notes (a tier that could not be fetched, a
	// provider that failed). Presence of errors never prevents a report:
	// degrade gracefully, render something useful.
	Errors []string `json:"errors,omitempty"`
}

// CommitInfo is a commit as the report presents it. Author emails are kept
// out of the JSON on purpose - the report is often shown beyond the dev team.
type CommitInfo struct {
	SHA         string    `json:"sha"`
	Subject     string    `json:"subject,omitempty"`
	Author      string    `json:"author,omitempty"`
	AuthorEmail string    `json:"-"`
	At          time.Time `json:"at,omitzero"`
	URL         string    `json:"url,omitempty"`
	Request     string    `json:"request,omitempty"`    // e.g. "!1234"
	RequestURL  string    `json:"requestUrl,omitempty"` // MR / PR link
	TaskID      string    `json:"taskId,omitempty"`     // e.g. "WS-123"
	TaskURL     string    `json:"taskUrl,omitempty"`
	Mine        bool      `json:"mine,omitempty"`
}

// GreenInfo is the Green tier: the latest commit with a passing pipeline.
type GreenInfo struct {
	CommitInfo
	Pipeline   string `json:"pipeline,omitempty"`
	BehindHead int    `json:"behindHead,omitempty"`
}

// ComponentReport is one component's Live answer. Live is nil only when
// nothing could be established at all (no provider and no Green to assume).
type ComponentReport struct {
	Name  string      `json:"name"`
	Live  *LiveReport `json:"live"`
	Error string      `json:"error,omitempty"`
}

// LiveReport is the Live tier for one component, with its confidence and how
// far it lags the other tiers. Keep the numbers separate: Live is N behind
// Green is M behind HEAD - never collapse them.
type LiveReport struct {
	CommitInfo
	Confidence  LiveConfidence `json:"confidence"`
	BuiltAt     time.Time      `json:"builtAt,omitzero"`
	BehindGreen int            `json:"behindGreen,omitempty"`
	BehindHead  int            `json:"behindHead,omitempty"`
}

// Drift flags components that report different Live commits when they should
// move together (your backend shipped, your web didn't).
type Drift struct {
	// Leader is the most-ahead component; the others are measured against it.
	Leader  string `json:"leader"`
	Message string `json:"message"` // e.g. "web 1 behind backend"
}
