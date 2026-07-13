package live_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amberpixels/onair"
	"github.com/amberpixels/onair/live"
)

func TestParseOutput(t *testing.T) {
	builtAt := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		in   string
		want onair.LiveInfo
		err  bool
	}{
		{name: "full sha", in: "9e8d7c6000000000000000000000000000000000\n",
			want: onair.LiveInfo{SHA: "9e8d7c6000000000000000000000000000000000", Confidence: onair.Reported}},
		{name: "short sha", in: "  a1b2c3d  ",
			want: onair.LiveInfo{SHA: "a1b2c3d", Confidence: onair.Reported}},
		{name: "image ref", in: "registry.example.com:5000/p44/backend:a1b2c3d",
			want: onair.LiveInfo{SHA: "a1b2c3d", Confidence: onair.Reported}},
		{name: "sha- tag convention", in: "ghcr.io/p44/web:sha-9e8d7c6",
			want: onair.LiveInfo{SHA: "9e8d7c6", Confidence: onair.Reported}},
		{name: "bare tag", in: "app:v1.2.3",
			want: onair.LiveInfo{SHA: "v1.2.3", Confidence: onair.Reported}},
		{name: "first token of noisy line", in: "a1b2c3d deployed 2h ago\nsecond line",
			want: onair.LiveInfo{SHA: "a1b2c3d", Confidence: onair.Reported}},
		{name: "json contract", in: `{"commit":"a1b2c3d","builtAt":"2026-07-13T00:00:00Z"}`,
			want: onair.LiveInfo{SHA: "a1b2c3d", BuiltAt: builtAt, Confidence: onair.Reported}},
		{name: "json without commit", in: `{"version":"1.2.3"}`, err: true},
		{name: "empty", in: "  \n ", err: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := live.ParseOutput(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCommand(t *testing.T) {
	c := &live.Command{Command: "echo a1b2c3d"}
	info, err := c.Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.SHA != "a1b2c3d" || info.Confidence != onair.Reported {
		t.Errorf("got %+v", info)
	}
}

func TestCommandFailure(t *testing.T) {
	c := &live.Command{Command: "echo kubectl-said-no >&2; exit 3"}
	if _, err := c.Live(context.Background()); err == nil {
		t.Fatal("expected error")
	} else if got := err.Error(); !strings.Contains(got, "kubectl-said-no") {
		t.Errorf("stderr not surfaced: %q", got)
	}
}

func TestProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"commit":"9e8d7c6","builtAt":"2026-07-13T00:00:00Z"}`))
	}))
	defer srv.Close()

	p := &live.Probe{URL: srv.URL}
	info, err := p.Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.SHA != "9e8d7c6" || info.Confidence != onair.Probed || info.BuiltAt.IsZero() {
		t.Errorf("got %+v", info)
	}
}

func TestProbeErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()

	p := &live.Probe{URL: srv.URL}
	if _, err := p.Live(context.Background()); err == nil {
		t.Fatal("expected error on non-200")
	}
}
