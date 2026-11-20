package onair

import (
	"context"
	"strings"
	"time"
)

// LiveConfidence states how much we actually know about the Live tier. It is
// a first-class field, not a boolean, so the renderer can be honest. Ranked
// highest first: Probed, Reported, Recorded, Assumed.
type LiveConfidence string

const (
	// Probed - the running artifact self-reported its commit (highest
	// fidelity).
	Probed LiveConfidence = "probed"
	// Reported - a user-supplied command asserted the running commit; truth
	// we did not observe ourselves.
	Reported LiveConfidence = "reported"
	// Recorded - the forge's deployment record for an environment: the commit
	// CI deployed, not what the host runs now.
	Recorded LiveConfidence = "recorded"
	// Assumed - no live source at all; Green stands in, clearly labeled.
	Assumed LiveConfidence = "assumed"
)

// confidenceRank orders confidences from strongest (0) to weakest.
var confidenceRank = map[LiveConfidence]int{Probed: 0, Reported: 1, Recorded: 2, Assumed: 3}

// Weaker reports whether c is a weaker claim than other. An unknown
// confidence ranks weakest, so it never passes for a stronger one.
func (c LiveConfidence) Weaker(other LiveConfidence) bool {
	rank := func(x LiveConfidence) int {
		if r, ok := confidenceRank[x]; ok {
			return r
		}
		return len(confidenceRank)
	}
	return rank(c) > rank(other)
}

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
	// Rollout is a deploy newer than SHA that has not succeeded; nil when
	// there is none or the provider cannot see deploys.
	Rollout *Rollout
}

// RolloutStatus is the state of a deploy that is not (yet) live.
type RolloutStatus string

const (
	// RolloutPending - the deploy is running.
	RolloutPending RolloutStatus = "pending"
	// RolloutFailed - the deploy failed, so the older Live commit kept running.
	RolloutFailed RolloutStatus = "failed"
)

// Rollout is a deploy of SHA that is in flight or failed.
type Rollout struct {
	SHA    string
	Status RolloutStatus
	At     time.Time // when the deploy started; zero when unknown
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
