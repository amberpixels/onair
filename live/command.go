package live

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/amberpixels/onair"
)

// Command is the universal live provider: run a user-supplied command and
// treat its stdout as the answer to "what is live". kubectl, flyctl,
// ssh + docker inspect, even the heroku CLI - all one seam, no per-platform
// adapters. Confidence is Reported: user-asserted truth we did not observe
// ourselves.
//
// Contract: stdout is a commit SHA or anything resolvable to one (a tag, an
// image ref whose tag is the SHA), or a JSON object {commit, builtAt}.
// A non-zero exit means Live is unavailable - the engine degrades to
// Assumed = Green.
type Command struct {
	Command string
	Dir     string // working directory; empty means inherit
}

func (c *Command) Live(ctx context.Context) (onair.LiveInfo, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", c.Command)
	cmd.Dir = c.Dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return onair.LiveInfo{}, fmt.Errorf("command: %w: %s", err, firstLine(string(ee.Stderr)))
		}
		return onair.LiveInfo{}, fmt.Errorf("command: %w", err)
	}
	return ParseOutput(string(out))
}

var shaRe = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// ParseOutput applies the command output contract: a JSON object
// {commit, builtAt}, or a single token that is a SHA, a tag, or an image ref
// whose tag part is the answer. The same parser backs the CLI's pipe form
// (`kubectl ... | onair --live-ref -`).
func ParseOutput(out string) (onair.LiveInfo, error) {
	s := strings.TrimSpace(out)
	if s == "" {
		return onair.LiveInfo{}, fmt.Errorf("command: empty output")
	}

	if strings.HasPrefix(s, "{") {
		var v struct {
			Commit  string `json:"commit"`
			BuiltAt string `json:"builtAt"`
		}
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return onair.LiveInfo{}, fmt.Errorf("command: bad JSON output: %w", err)
		}
		if v.Commit == "" {
			return onair.LiveInfo{}, fmt.Errorf(`command: JSON output has no "commit"`)
		}
		info := onair.LiveInfo{SHA: v.Commit, Confidence: onair.Reported}
		if t, err := time.Parse(time.RFC3339, v.BuiltAt); err == nil {
			info.BuiltAt = t
		}
		return info, nil
	}

	token := strings.Fields(firstLine(s))[0]
	if !shaRe.MatchString(token) {
		// An image ref: the tag after the last ':' carries the commit
		// (registry hosts may hold a ':' too, hence last).
		if i := strings.LastIndexByte(token, ':'); i >= 0 && i < len(token)-1 {
			token = token[i+1:]
		}
		// Tags stamped as "sha-a1b2c3d" (a common CI convention).
		token = strings.TrimPrefix(token, "sha-")
	}
	return onair.LiveInfo{SHA: token, Confidence: onair.Reported}, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
