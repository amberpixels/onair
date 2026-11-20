package render_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amberpixels/onair"
	"github.com/amberpixels/onair/render"
)

func report() *onair.Report {
	at := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	head := onair.CommitInfo{
		SHA: "a1b2c3d000000000000000000000000000000000", Subject: "Fix the thing",
		Author: "Alice", At: at, Request: "!1234", RequestURL: "https://gitlab.com/x/-/merge_requests/1234",
	}
	liveCI := onair.CommitInfo{
		SHA: "9e8d7c6000000000000000000000000000000000", Subject: "Add the widget API",
		Author: "Bob", At: at.Add(-time.Hour), Mine: true, TaskID: "PROJ-42",
	}
	return &onair.Report{
		Project: "shop", Environment: "prod",
		Forge: onair.ForgeInfo{Kind: "gitlab", Host: "gitlab.com", Repo: "acme/shop", Branch: "main"},
		Head:  &head,
		Green: &onair.GreenInfo{CommitInfo: head, Pipeline: "success"},
		Components: []onair.ComponentReport{
			{Name: "backend", Live: &onair.LiveReport{
				CommitInfo: liveCI, Confidence: onair.Probed, BehindGreen: 1, BehindHead: 1,
			}},
			{Name: "web", Live: &onair.LiveReport{
				CommitInfo: onair.CommitInfo{SHA: head.SHA}, Confidence: onair.Assumed,
			}},
		},
		Drift:       &onair.Drift{Leader: "backend", Message: "web 1 behind backend"},
		Attribution: onair.AttributionAuto,
	}
}

func TestTTY(t *testing.T) {
	var buf strings.Builder
	err := render.TTY(&buf, report(), render.TTYOptions{
		Color: false,
		Now:   time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		"shop · prod",
		"gitlab.com/acme/shop",
		"HEAD    a1b2c3d (2h ago) by Alice",
		"★ == HEAD",
		"→ Fix the thing • ↗ !1234",
		"Live    9e8d7c6 (3h ago) by Bob",
		"↓ 1 behind green",
		"1 behind main",
		"✓ yours",
		"backend 9e8d7c6 · web a1b2c3d (assumed)",
		"⚠ web 1 behind backend",
		"→ Add the widget API • ⧉ PROJ-42",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tty output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("Color: false must not emit ANSI escapes")
	}
}

// When HEAD == Green == every component's Live, the report collapses to a
// single "Deployed: … ★ current" line and drops the per-tier breakdown.
func TestTTYConverged(t *testing.T) {
	at := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)
	head := onair.CommitInfo{
		SHA: "6a9af3d1c00000000000000000000000000000000", Subject: "Stepper face",
		Author: "Bob", At: at, Mine: true, Request: "!4076",
	}
	r := &onair.Report{
		Project: "shop", Environment: "prod",
		Forge: onair.ForgeInfo{Kind: "gitlab", Host: "gitlab.com", Repo: "acme/shop", Branch: "main"},
		Head:  &head,
		Green: &onair.GreenInfo{CommitInfo: head, Pipeline: "success"},
		Components: []onair.ComponentReport{
			{Name: "backend", Live: &onair.LiveReport{CommitInfo: head, Confidence: onair.Probed}},
			{Name: "web", Live: &onair.LiveReport{CommitInfo: head, Confidence: onair.Probed}},
		},
		Attribution: onair.AttributionAuto,
	}

	var buf strings.Builder
	if err := render.TTY(&buf, r, render.TTYOptions{Now: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.Contains(out, "Deployed: 6a9af3d (1h ago) by Bob") {
		t.Errorf("missing collapsed deployed line:\n%s", out)
	}
	for _, want := range []string{"★ current", "✓ yours", "→ Stepper face • ↗ !4076"} {
		if !strings.Contains(out, want) {
			t.Errorf("converged output missing %q:\n%s", want, out)
		}
	}
	// The whole point of the collapse: no multi-tier breakdown.
	for _, absent := range []string{"HEAD", "Green", "Live", "== HEAD", "in sync"} {
		if strings.Contains(out, absent) {
			t.Errorf("converged output should not contain %q:\n%s", absent, out)
		}
	}
}

// A recorded Live with newer deploys in flight or failed: the report must not
// collapse, and each rollout gets its own line.
func TestTTYRecordedRollout(t *testing.T) {
	at := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)
	head := onair.CommitInfo{SHA: "6a9af3d1c00000000000000000000000000000000", Subject: "Stepper face", At: at}
	r := &onair.Report{
		Project: "shop", Environment: "prod",
		Forge: onair.ForgeInfo{Kind: "gitlab", Repo: "acme/shop", Branch: "main"},
		Head:  &head,
		Green: &onair.GreenInfo{CommitInfo: head, Pipeline: "success"},
		Components: []onair.ComponentReport{
			{Name: "backend", Live: &onair.LiveReport{
				CommitInfo: head, Confidence: onair.Recorded,
				Rollout: &onair.RolloutReport{
					CommitInfo: onair.CommitInfo{SHA: "b0b0b0b"}, Status: onair.RolloutPending, StartedAt: at.Add(30 * time.Minute),
				},
			}},
			{Name: "web", Live: &onair.LiveReport{
				CommitInfo: head, Confidence: onair.Recorded,
				Rollout: &onair.RolloutReport{CommitInfo: onair.CommitInfo{SHA: "dead000"}, Status: onair.RolloutFailed},
			}},
		},
	}

	var buf strings.Builder
	if err := render.TTY(&buf, r, render.TTYOptions{Now: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"(recorded)",
		"backend ⟳ deploying b0b0b0b (30m ago)",
		"web ✗ deploy of dead000 failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Deployed:") {
		t.Errorf("a rollout must block the collapsed view:\n%s", out)
	}

	// A probed leader beside a recorded component: the cell carries the label.
	r.Components[0].Live.Confidence = onair.Probed
	buf.Reset()
	if err := render.TTY(&buf, r, render.TTYOptions{Now: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "web 6a9af3d (recorded)") {
		t.Errorf("non-leader recorded component lost its label:\n%s", out)
	}

	// Without rollouts the collapse returns, labeled with the weakest claim.
	for i := range r.Components {
		r.Components[i].Live.Rollout = nil
	}
	buf.Reset()
	if err := render.TTY(&buf, r, render.TTYOptions{Now: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "Deployed:") || !strings.Contains(out, "(recorded)") {
		t.Errorf("expected a collapsed recorded line:\n%s", out)
	}

	buf.Reset()
	r.Components[0].Live.Rollout = &onair.RolloutReport{CommitInfo: onair.CommitInfo{SHA: "b0b0b0b"}, Status: onair.RolloutPending}
	if err := render.JSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"confidence": "recorded"`, `"rollout": {`, `"status": "pending"`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("JSON missing %s:\n%s", want, buf.String())
		}
	}
}

func TestJSON(t *testing.T) {
	var buf strings.Builder
	if err := render.JSON(&buf, report()); err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(buf.String()), &v); err != nil {
		t.Fatal(err)
	}
	if v["project"] != "shop" || v["drift"] == nil {
		t.Errorf("unexpected JSON: %v", v)
	}
	// The report is shown beyond the dev team: no author emails in JSON.
	if strings.Contains(buf.String(), "@") {
		t.Errorf("JSON must not leak emails:\n%s", buf.String())
	}
}

// A component in a restart loop on HEAD is not "current": the report keeps
// the full breakdown and marks the component with its state.
func TestTTYNotServing(t *testing.T) {
	at := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)
	head := onair.CommitInfo{SHA: "6a9af3d1c00000000000000000000000000000000", Subject: "Stepper face", At: at}
	r := &onair.Report{
		Project: "shop", Environment: "prod",
		Forge: onair.ForgeInfo{Kind: "gitlab", Repo: "acme/shop", Branch: "main"},
		Head:  &head,
		Green: &onair.GreenInfo{CommitInfo: head},
		Components: []onair.ComponentReport{
			{Name: "backend", Live: &onair.LiveReport{CommitInfo: head, Confidence: onair.Reported, State: "restarting"}},
			{Name: "web", Live: &onair.LiveReport{CommitInfo: head, Confidence: onair.Reported, State: "running"}},
		},
	}

	var buf strings.Builder
	if err := render.TTY(&buf, r, render.TTYOptions{Now: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	for _, want := range []string{
		"HEAD", "Green", "Live",
		"backend 6a9af3d ✗ restarting · web 6a9af3d   ✗ backend not serving",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	for _, absent := range []string{"★ current", "in sync"} {
		if strings.Contains(out, absent) {
			t.Errorf("a non-serving component must not read as %q:\n%s", absent, out)
		}
	}
	if strings.Contains(out, "✗ running") {
		t.Errorf("a serving state must not be marked:\n%s", out)
	}
}

func TestJSONState(t *testing.T) {
	r := report()
	r.Components[0].Live.State = "unhealthy"

	var buf strings.Builder
	if err := render.JSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var v struct {
		Components []struct {
			Live map[string]any `json:"live"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &v); err != nil {
		t.Fatal(err)
	}
	if got := v.Components[0].Live["state"]; got != "unhealthy" {
		t.Errorf("backend state = %v, want unhealthy", got)
	}
	if _, ok := v.Components[1].Live["state"]; ok {
		t.Errorf("unknown state must be omitted: %v", v.Components[1].Live)
	}
}

// A converged deploy whose weakest component is Reported (the Compose reader
// through `command`) must say so on the collapsed line, not read as probed.
func TestTTYConvergedReported(t *testing.T) {
	at := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)
	head := onair.CommitInfo{SHA: "6a9af3d1c00000000000000000000000000000000", Subject: "Stepper face", At: at}
	r := &onair.Report{
		Project: "shop", Environment: "prod",
		Forge: onair.ForgeInfo{Kind: "gitlab", Repo: "acme/shop", Branch: "main"},
		Head:  &head,
		Green: &onair.GreenInfo{CommitInfo: head, Pipeline: "success"},
		Components: []onair.ComponentReport{
			{Name: "backend", Live: &onair.LiveReport{CommitInfo: head, Confidence: onair.Probed}},
			{Name: "web", Live: &onair.LiveReport{CommitInfo: head, Confidence: onair.Reported}},
		},
	}

	var buf strings.Builder
	if err := render.TTY(&buf, r, render.TTYOptions{Now: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Deployed:") || !strings.Contains(out, "★ current · (reported)") {
		t.Errorf("collapsed line must carry the reported label:\n%s", out)
	}
}

// A provider that failed falls back to Assumed, so the component has a Live;
// its error must still reach the terminal on both the collapsed and the full
// view.
func TestTTYComponentErrors(t *testing.T) {
	at := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)
	head := onair.CommitInfo{SHA: "6a9af3d1c00000000000000000000000000000000", Subject: "Stepper face", At: at}
	r := &onair.Report{
		Project: "shop", Environment: "prod",
		Forge: onair.ForgeInfo{Kind: "gitlab", Repo: "acme/shop", Branch: "main"},
		Head:  &head,
		Green: &onair.GreenInfo{CommitInfo: head, Pipeline: "success"},
		Components: []onair.ComponentReport{
			{Name: "backend", Live: &onair.LiveReport{CommitInfo: head, Confidence: onair.Assumed},
				Error: `no successful deployment in environment "prodution"`},
		},
		Errors: []string{"identity: boom"},
	}

	render1 := func() string {
		t.Helper()
		var buf strings.Builder
		if err := render.TTY(&buf, r, render.TTYOptions{Now: at.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	want := `! backend: no successful deployment in environment "prodution"`
	if out := render1(); !strings.Contains(out, "Deployed:") || !strings.Contains(out, want) || !strings.Contains(out, "! identity: boom") {
		t.Errorf("collapsed view lost an error note:\n%s", out)
	}

	r.Green = &onair.GreenInfo{CommitInfo: onair.CommitInfo{SHA: "b0b0b0b"}}
	r.Components[0].Live.CommitInfo = r.Green.CommitInfo
	if out := render1(); !strings.Contains(out, "Live") || !strings.Contains(out, want) {
		t.Errorf("full view lost the component error:\n%s", out)
	}

	// No Live at all: each error still prints exactly once.
	r.Components[0].Live = nil
	if out := render1(); strings.Count(out, "prodution") != 1 {
		t.Errorf("component error should print once:\n%s", out)
	}
}

// A cell is labeled whenever its claim differs from the leader's, so a probed
// component beside a weaker drift leader does not read as the leader's claim.
func TestTTYStrongerCellLabeled(t *testing.T) {
	r := report()
	r.Components[0].Live.Confidence = onair.Recorded
	r.Components[1].Live.Confidence = onair.Probed

	var buf strings.Builder
	if err := render.TTY(&buf, r, render.TTYOptions{Now: time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "backend 9e8d7c6 · web a1b2c3d (probed)") {
		t.Errorf("a stronger cell should carry its label:\n%s", out)
	}
}
