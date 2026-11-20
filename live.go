package onair

import (
	"context"
	"strings"
	"time"
)

// LiveConfidence states how much we actually know about the Live tier. It is
// a first-class field, not a boolean, so the renderer can be honest.
type LiveConfidence string

const (
	// Probed - the running artifact self-reported its commit (highest
	// fidelity).
	Probed LiveConfidence = "probed"
	// Reported - a user-supplied command asserted the running commit; truth
	// we did not observe ourselves.
	Reported LiveConfidence = "reported"
	// Assumed - no live source at all; Green stands in, clearly labeled.
	Assumed LiveConfidence = "assumed"
)

// LiveInfo is what a LiveProvider can know on its own: a commit-ish and,
// sometimes, when it was built. The engine resolves the rest through the
// Forge.
type LiveInfo struct {
	// SHA is a full or short commit SHA, or anything the Forge can resolve
	// to one (a tag, an image-ref suffix).
	SHA     string
	BuiltAt time.Time // zero when the provider cannot know
	// State is the container state as the provider reported it, verbatim.
	// Empty (unknown), "running" and "healthy" mean serving; see Serving.
	State      string
	Confidence LiveConfidence
}

// Serving reports whether a provider-reported state means the component
// serves traffic. An unknown (empty) state counts as serving, so providers
// that report no state render as before.
func Serving(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "running", "healthy":
		return true
	}
	return false
}

// LiveProvider answers what is actually running for one component. A nil
// provider means the Assumed fallback: Live is taken to be Green, labeled as
// an assumption - the engine implements that itself because only it knows
// Green.
type LiveProvider interface {
	Live(ctx context.Context) (LiveInfo, error)
}

// Component is a named deployable unit (backend, web, worker...) with its own
// live source.
type Component struct {
	Name string
	Live LiveProvider // nil -> Assumed
}
