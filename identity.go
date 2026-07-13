package onair

import "context"

// Identity answers who is viewing the report, for "yours" attribution. The
// CLI reads git config user.email; a host application hands in the logged-in
// user's email.
type Identity interface {
	// ViewerEmails returns the viewer's known emails; may be empty.
	ViewerEmails(ctx context.Context) ([]string, error)
}

// AttributionMode gates the "yours" annotations.
type AttributionMode string

const (
	// AttributionOff - never annotate.
	AttributionOff AttributionMode = "off"
	// AttributionOn - always annotate against the configured identity.
	AttributionOn AttributionMode = "on"
	// AttributionAuto - annotate only if the viewer's email resolves to a
	// known git author in recent history; the noise disappears for people it
	// means nothing to.
	AttributionAuto AttributionMode = "auto"
)
