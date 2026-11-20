// Package live provides the host-side LiveProvider implementations: probe
// (the /-/version convention), command (the universal adapter - any CLI's
// output is the answer) and static (a ref handed in directly, e.g. piped to
// the CLI). Deployment records live in the gitlab package, beside the forge
// that keeps them, and the assumed fallback lives in the core engine, because
// only the engine knows Green.
package live

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/amberpixels/onair"
)

// Probe reads a /-/version-style endpoint where the running artifact
// self-reports: {"commit":"a1b2c3d","builtAt":"..."}. Highest fidelity - this
// is the app itself speaking.
type Probe struct {
	URL string
	// HTTPClient defaults to a client with a 10s timeout.
	HTTPClient *http.Client
}

func (p *Probe) Live(ctx context.Context) (onair.LiveInfo, error) {
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return onair.LiveInfo{}, fmt.Errorf("probe %s: %w", p.URL, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return onair.LiveInfo{}, fmt.Errorf("probe %s: %w", p.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return onair.LiveInfo{}, fmt.Errorf("probe %s: %s", p.URL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return onair.LiveInfo{}, fmt.Errorf("probe %s: %w", p.URL, err)
	}
	info, err := decodeVersion(body)
	if err != nil {
		return onair.LiveInfo{}, fmt.Errorf("probe %s: bad version payload: %w", p.URL, err)
	}
	if info.SHA == "" {
		return onair.LiveInfo{}, fmt.Errorf(`probe %s: version payload has no "commit"`, p.URL)
	}
	// An app that answers its probe is serving, so a state it sends is not
	// part of the probe contract.
	return onair.LiveInfo{SHA: info.SHA, BuiltAt: info.BuiltAt, Confidence: onair.Probed}, nil
}

// decodeVersion decodes the {commit, builtAt, state} payload the probe and
// command contracts share. An unparsable builtAt stays zero; callers wrap the
// error and check for a missing commit with their own wording.
func decodeVersion(data []byte) (onair.LiveInfo, error) {
	var v struct {
		Commit  string `json:"commit"`
		BuiltAt string `json:"builtAt"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return onair.LiveInfo{}, err
	}
	info := onair.LiveInfo{SHA: v.Commit, State: strings.TrimSpace(v.State)}
	if t, err := time.Parse(time.RFC3339, v.BuiltAt); err == nil {
		info.BuiltAt = t
	}
	return info, nil
}
