package onair

import (
	"regexp"
	"strings"
	"time"
)

// Ref is a resolved commit reference on the tracked branch.
type Ref struct {
	SHA         string
	Subject     string
	AuthorName  string
	AuthorEmail string
	At          time.Time
	URL         string
}

// IsZero reports whether the ref carries no commit at all.
func (r Ref) IsZero() bool { return r.SHA == "" }

// SameSHA reports whether two SHAs name the same commit, tolerating
// abbreviated forms: a probe typically reports a short SHA while a forge
// returns the full one. Anything shorter than 7 hex characters must match
// exactly - prefixes that short are not trustworthy.
func SameSHA(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == "" || b == "" {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(a) < 7 {
		return a == b
	}
	return strings.HasPrefix(b, a)
}

var shaRe = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// LooksLikeSHA reports whether s has the shape of a full or abbreviated
// commit SHA (7 to 40 hex characters), as opposed to a tag or an image ref.
func LooksLikeSHA(s string) bool { return shaRe.MatchString(s) }

// ShortSHA abbreviates a SHA for display.
func ShortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
