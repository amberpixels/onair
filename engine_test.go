package onair_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amberpixels/onair"
)

var (
	at = time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)

	refHead = onair.Ref{
		SHA: "a1b2c3d000000000000000000000000000000000", Subject: "Fix the thing (!1234)",
		AuthorName: "Alice", AuthorEmail: "alice@example.com", At: at,
	}
	refLive = onair.Ref{
		SHA: "9e8d7c6000000000000000000000000000000000", Subject: "WS-42: Add the widget API (!1230)",
		AuthorName: "Eugene", AuthorEmail: "eugene@example.com", At: at.Add(-time.Hour),
	}
	refOld = onair.Ref{
		SHA: "5f4e3d2000000000000000000000000000000000", Subject: "Older",
		AuthorName: "Alice", AuthorEmail: "alice@example.com", At: at.Add(-2 * time.Hour),
	}
)

type fakeForge struct {
	head, green onair.Ref
	headErr     error
	greenErr    error
	behind      map[string]int // "from..to" (short SHAs) -> n
	recent      []onair.Ref
}

func (f *fakeForge) Head(ctx context.Context, branch string) (onair.Ref, error) {
	return f.head, f.headErr
}

func (f *fakeForge) LatestGreen(ctx context.Context, branch string) (onair.Ref, error) {
	return f.green, f.greenErr
}

func (f *fakeForge) Resolve(ctx context.Context, sha string) (onair.Ref, error) {
	for _, r := range append([]onair.Ref{f.head, f.green}, f.recent...) {
		if onair.SameSHA(r.SHA, sha) {
			return r, nil
		}
	}
	return onair.Ref{}, errors.New("unknown sha " + sha)
}

func (f *fakeForge) Behind(ctx context.Context, from, to string) (int, error) {
	key := onair.ShortSHA(from) + ".." + onair.ShortSHA(to)
	if n, ok := f.behind[key]; ok {
		return n, nil
	}
	return 0, fmt.Errorf("no behind fixture for %s", key)
}

func (f *fakeForge) Recent(ctx context.Context, branch string, limit int) ([]onair.Ref, error) {
	return f.recent, nil
}

func (f *fakeForge) CommitURL(sha string) string { return "https://forge.test/c/" + sha }
func (f *fakeForge) RequestURL(id string) string { return "https://forge.test/mr/" + id }

type fakeLive struct {
	info onair.LiveInfo
	err  error
}

func (f fakeLive) Live(context.Context) (onair.LiveInfo, error) { return f.info, f.err }

func collect(t *testing.T, p onair.Params) *onair.Report {
	t.Helper()
	r, err := onair.Collect(context.Background(), p)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return r
}

func TestCollectInSync(t *testing.T) {
	forge := &fakeForge{head: refHead, green: refHead}
	r := collect(t, onair.Params{
		Project: "p44", Environment: "prod", Forge: forge,
		Components: []onair.Component{
			{Name: "backend", Live: fakeLive{info: onair.LiveInfo{SHA: "a1b2c3d", Confidence: onair.Probed}}},
			{Name: "web", Live: fakeLive{info: onair.LiveInfo{SHA: "a1b2c3d", Confidence: onair.Probed}}},
		},
		Attribution: onair.AttributionOff,
	})

	if r.Head == nil || r.Green == nil {
		t.Fatalf("expected head and green, got %+v", r)
	}
	if r.Drift != nil {
		t.Errorf("expected no drift, got %+v", r.Drift)
	}
	for _, c := range r.Components {
		if c.Live == nil || c.Live.Confidence != onair.Probed {
			t.Errorf("%s: expected probed live, got %+v", c.Name, c.Live)
		}
		if c.Live.BehindGreen != 0 || c.Live.BehindHead != 0 {
			t.Errorf("%s: expected in sync, got %+v", c.Name, c.Live)
		}
		// Short probe SHA must resolve to the full forge ref.
		if c.Live.Subject == "" {
			t.Errorf("%s: live not resolved through forge: %+v", c.Name, c.Live)
		}
	}
}

func TestCollectStalledDeploy(t *testing.T) {
	forge := &fakeForge{
		head: refHead, green: refHead,
		recent: []onair.Ref{refHead, refLive},
		behind: map[string]int{"9e8d7c6..a1b2c3d": 1},
	}
	r := collect(t, onair.Params{
		Forge: forge, Attribution: onair.AttributionOff,
		Components: []onair.Component{
			{Name: "backend", Live: fakeLive{info: onair.LiveInfo{SHA: "9e8d7c6", Confidence: onair.Probed}}},
		},
	})

	lr := r.Components[0].Live
	if lr.BehindGreen != 1 || lr.BehindHead != 1 {
		t.Errorf("expected live 1 behind green and head, got %+v", lr)
	}
}

func TestCollectAssumedFallbacks(t *testing.T) {
	forge := &fakeForge{head: refHead, green: refHead}
	r := collect(t, onair.Params{
		Forge: forge, Attribution: onair.AttributionOff,
		Components: []onair.Component{
			{Name: "no-provider"}, // nil provider -> assumed
			{Name: "broken", Live: fakeLive{err: errors.New("probe timed out")}},
		},
	})

	for _, c := range r.Components {
		if c.Live == nil || c.Live.Confidence != onair.Assumed {
			t.Fatalf("%s: expected assumed live, got %+v", c.Name, c.Live)
		}
		if !onair.SameSHA(c.Live.SHA, refHead.SHA) {
			t.Errorf("%s: assumed live should be green, got %s", c.Name, c.Live.SHA)
		}
	}
	if r.Components[1].Error != "probe timed out" {
		t.Errorf("expected provider error recorded, got %q", r.Components[1].Error)
	}
}

func TestCollectNoGreenNoProvider(t *testing.T) {
	forge := &fakeForge{head: refHead, greenErr: errors.New("no successful pipeline")}
	r := collect(t, onair.Params{
		Forge: forge, Attribution: onair.AttributionOff,
		Components: []onair.Component{{Name: "backend"}},
	})

	if r.Green != nil {
		t.Errorf("expected no green tier, got %+v", r.Green)
	}
	if r.Components[0].Live != nil {
		t.Errorf("expected no live, got %+v", r.Components[0].Live)
	}
	if r.Components[0].Error == "" {
		t.Error("expected a degradation note on the component")
	}
	if len(r.Errors) == 0 || !strings.Contains(r.Errors[0], "green") {
		t.Errorf("expected a green error note, got %v", r.Errors)
	}
}

func TestCollectDrift(t *testing.T) {
	forge := &fakeForge{
		head: refHead, green: refHead,
		recent: []onair.Ref{refHead, refLive, refOld},
		behind: map[string]int{
			"5f4e3d2..9e8d7c6": 1,
			"9e8d7c6..a1b2c3d": 1,
			"5f4e3d2..a1b2c3d": 2,
		},
	}
	r := collect(t, onair.Params{
		Forge: forge, Attribution: onair.AttributionOff,
		Components: []onair.Component{
			{Name: "backend", Live: fakeLive{info: onair.LiveInfo{SHA: "9e8d7c6", Confidence: onair.Probed}}},
			{Name: "web", Live: fakeLive{info: onair.LiveInfo{SHA: "5f4e3d2", Confidence: onair.Probed}}},
		},
	})

	if r.Drift == nil {
		t.Fatal("expected drift")
	}
	if r.Drift.Leader != "backend" {
		t.Errorf("expected backend to lead (newest live), got %q", r.Drift.Leader)
	}
	if r.Drift.Message != "web 1 behind backend" {
		t.Errorf("unexpected drift message %q", r.Drift.Message)
	}
}

func TestCollectAttribution(t *testing.T) {
	newForge := func() *fakeForge {
		return &fakeForge{head: refHead, green: refHead, recent: []onair.Ref{refHead, refLive}}
	}
	eugene := fakeIdentity{"amber.pixels.io@gmail.com"}
	aliases := map[string][]string{"eugene": {"amber.pixels.io@gmail.com", "eugene@example.com"}}
	liveComp := []onair.Component{
		{Name: "backend", Live: fakeLive{info: onair.LiveInfo{SHA: "9e8d7c6", Confidence: onair.Probed}}},
	}

	t.Run("on with alias map", func(t *testing.T) {
		forge := newForge()
		forge.behind = map[string]int{"9e8d7c6..a1b2c3d": 1}
		r := collect(t, onair.Params{
			Forge: forge, Attribution: onair.AttributionOn,
			Identity: eugene, Identities: aliases, Components: liveComp,
		})
		if !r.Components[0].Live.Mine {
			t.Error("expected live commit to be mine via the alias map")
		}
		if r.Head.Mine {
			t.Error("head is Alice's, not mine")
		}
	})

	t.Run("auto gate open", func(t *testing.T) {
		forge := newForge()
		forge.behind = map[string]int{"9e8d7c6..a1b2c3d": 1}
		r := collect(t, onair.Params{
			Forge: forge, Attribution: onair.AttributionAuto,
			Identity: eugene, Identities: aliases, Components: liveComp,
		})
		if !r.Components[0].Live.Mine {
			t.Error("gate should be open: eugene@example.com authored recent history")
		}
	})

	t.Run("auto gate closed for non-authors", func(t *testing.T) {
		forge := newForge()
		forge.behind = map[string]int{"9e8d7c6..a1b2c3d": 1}
		r := collect(t, onair.Params{
			Forge: forge, Attribution: onair.AttributionAuto,
			Identity: fakeIdentity{"random.admin@example.com"}, Components: liveComp,
		})
		if r.Components[0].Live.Mine {
			t.Error("gate should be closed: viewer is not a recent author")
		}
	})

	t.Run("off", func(t *testing.T) {
		forge := newForge()
		forge.behind = map[string]int{"9e8d7c6..a1b2c3d": 1}
		r := collect(t, onair.Params{
			Forge: forge, Attribution: onair.AttributionOff,
			Identity: eugene, Identities: aliases, Components: liveComp,
		})
		if r.Components[0].Live.Mine {
			t.Error("off must never annotate")
		}
	})
}

func TestCollectLinks(t *testing.T) {
	forge := &fakeForge{
		head: refHead, green: refHead,
		recent: []onair.Ref{refHead, refLive},
		behind: map[string]int{"9e8d7c6..a1b2c3d": 1},
	}
	r := collect(t, onair.Params{
		Forge: forge, Attribution: onair.AttributionOff,
		Task: &onair.TaskConfig{Pattern: `WS-\d+`, URL: "https://tasks.test/{id}"},
		Components: []onair.Component{
			{Name: "backend", Live: fakeLive{info: onair.LiveInfo{SHA: "9e8d7c6", Confidence: onair.Probed}}},
		},
	})

	if r.Head.Request != "!1234" || r.Head.RequestURL != "https://forge.test/mr/1234" {
		t.Errorf("head MR link not annotated: %+v", r.Head)
	}
	lr := r.Components[0].Live
	if lr.TaskID != "WS-42" || lr.TaskURL != "https://tasks.test/WS-42" {
		t.Errorf("live task link not annotated: %+v", lr.CommitInfo)
	}
}

func TestCollectUnresolvableLiveStillReports(t *testing.T) {
	forge := &fakeForge{head: refHead, green: refHead}
	r := collect(t, onair.Params{
		Forge: forge, Attribution: onair.AttributionOff,
		Components: []onair.Component{
			{Name: "backend", Live: fakeLive{info: onair.LiveInfo{SHA: "deadbeef", Confidence: onair.Reported}}},
		},
	})

	lr := r.Components[0].Live
	if lr == nil || lr.SHA != "deadbeef" {
		t.Fatalf("expected a bare-SHA live report, got %+v", lr)
	}
	if lr.Confidence != onair.Reported {
		t.Errorf("expected reported confidence, got %s", lr.Confidence)
	}
}

type fakeIdentity []string

func (f fakeIdentity) ViewerEmails(context.Context) ([]string, error) { return f, nil }
