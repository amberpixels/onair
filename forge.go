package onair

import "context"

// Forge is the umbrella seam for a git host (GitLab, GitHub, Gitea...). It
// answers the abstract questions every host can answer; concrete
// implementations are swappable, so GitLab-first does not mean GitLab-only.
type Forge interface {
	// Head returns the latest commit on the tracked branch (the HEAD tier).
	Head(ctx context.Context, branch string) (Ref, error)
	// LatestGreen returns the newest commit on branch whose CI pipeline
	// passed (the Green tier).
	LatestGreen(ctx context.Context, branch string) (Ref, error)
	// Resolve returns full commit metadata for a SHA, short SHA or tag.
	Resolve(ctx context.Context, sha string) (Ref, error)
	// Behind counts how many commits `to` has that `from` does not.
	Behind(ctx context.Context, from, to string) (int, error)
	// Recent returns up to limit latest commits on branch. Used by the
	// attribution auto-gate to build the recent-history author set.
	Recent(ctx context.Context, branch string, limit int) ([]Ref, error)
	// CommitURL returns a browsable URL for a commit.
	CommitURL(sha string) string
	// RequestURL returns a browsable URL for a merge/pull request id.
	RequestURL(id string) string
}

// ForgeInfo describes the forge for the report header; it carries no
// behavior.
type ForgeInfo struct {
	Kind   string `json:"kind"`
	Host   string `json:"host,omitempty"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}
