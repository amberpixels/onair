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
		Author: "Eugene", At: at.Add(-time.Hour), Mine: true, TaskID: "WS-42",
	}
	return &onair.Report{
		Project: "p44", Environment: "prod",
		Forge: onair.ForgeInfo{Kind: "gitlab", Host: "gitlab.com", Repo: "amberpixels/p44", Branch: "main"},
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
		"p44 · prod",
		"gitlab.com/amberpixels/p44",
		"HEAD    a1b2c3d (2h ago) by Alice",
		"★ == HEAD",
		"→ Fix the thing • ↗ !1234",
		"Live    9e8d7c6 (3h ago) by Eugene",
		"↓ 1 behind green",
		"1 behind main",
		"✓ yours",
		"backend 9e8d7c6 · web a1b2c3d (assumed)",
		"⚠ web 1 behind backend",
		"→ Add the widget API • ⧉ WS-42",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tty output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("Color: false must not emit ANSI escapes")
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
	if v["project"] != "p44" || v["drift"] == nil {
		t.Errorf("unexpected JSON: %v", v)
	}
	// The report is shown beyond the dev team: no author emails in JSON.
	if strings.Contains(buf.String(), "@") {
		t.Errorf("JSON must not leak emails:\n%s", buf.String())
	}
}
