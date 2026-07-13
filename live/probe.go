// Package live provides the LiveProvider implementations: probe (the
// /-/version convention), command (the universal adapter - any CLI's output
// is the answer) and static (a ref handed in directly, e.g. piped to the
// CLI). The assumed fallback lives in the core engine, because only the
// engine knows Green.
package live

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	var v struct {
		Commit  string `json:"commit"`
		BuiltAt string `json:"builtAt"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return onair.LiveInfo{}, fmt.Errorf("probe %s: bad version payload: %w", p.URL, err)
	}
	if v.Commit == "" {
		return onair.LiveInfo{}, fmt.Errorf(`probe %s: version payload has no "commit"`, p.URL)
	}
	info := onair.LiveInfo{SHA: v.Commit, Confidence: onair.Probed}
	if t, err := time.Parse(time.RFC3339, v.BuiltAt); err == nil {
		info.BuiltAt = t
	}
	return info, nil
}
