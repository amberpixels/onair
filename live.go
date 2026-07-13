package onair

import (
	"context"
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
	SHA        string
	BuiltAt    time.Time // zero when the provider cannot know
	Confidence LiveConfidence
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
