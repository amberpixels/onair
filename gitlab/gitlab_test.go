package gitlab_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/amberpixels/onair"
	"github.com/amberpixels/onair/gitlab"
)

const commitJSON = `{
	"id": "a1b2c3d000000000000000000000000000000000",
	"title": "Fix the thing (!1234)",
	"author_name": "Alice",
	"author_email": "alice@example.com",
	"created_at": "2026-07-13T08:00:00Z",
	"web_url": "https://gitlab.test/amberpixels/p44/-/commit/a1b2c3d"
}`

func newClient(t *testing.T) *gitlab.Client {
	t.Helper()
	mux := http.NewServeMux()
	project := "/api/v4/projects/amberpixels%2Fp44"
	mux.HandleFunc(project+"/repository/commits", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref_name") != "main" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, "[%s]", commitJSON)
	})
	mux.HandleFunc(project+"/repository/commits/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "secret" {
			http.Error(w, "401", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, commitJSON)
	})
	mux.HandleFunc(project+"/pipelines", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "success" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `[{"sha": "a1b2c3d000000000000000000000000000000000"}]`)
	})
	mux.HandleFunc(project+"/repository/compare", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commits": [{}, {}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// The raw path must stay escaped (%2F); the client relies on that.
	return gitlab.NewClient(srv.URL, "amberpixels/p44", "secret")
}

func TestHead(t *testing.T) {
	client := newClient(t)
	ref, err := client.Head(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if ref.AuthorEmail != "alice@example.com" || ref.Subject != "Fix the thing (!1234)" {
		t.Errorf("got %+v", ref)
	}
}

func TestLatestGreenResolvesCommit(t *testing.T) {
	client := newClient(t)
	ref, err := client.LatestGreen(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref.SHA, "a1b2c3d") || ref.AuthorName != "Alice" {
		t.Errorf("green not resolved to full commit: %+v", ref)
	}
}

func TestBehind(t *testing.T) {
	client := newClient(t)
	n, err := client.Behind(context.Background(), "9e8d7c6", "a1b2c3d")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestURLs(t *testing.T) {
	client := gitlab.NewClient("", "amberpixels/p44", "")
	if got := client.CommitURL("a1b2c3d"); got != "https://gitlab.com/amberpixels/p44/-/commit/a1b2c3d" {
		t.Error(got)
	}
	if got := client.RequestURL("1234"); got != "https://gitlab.com/amberpixels/p44/-/merge_requests/1234" {
		t.Error(got)
	}
	if got := client.Host(); got != "gitlab.com" {
		t.Error(got)
	}
}

// deploymentsClient serves the deployments endpoint: window answers the
// unfiltered call, success the status=success fallback.
func deploymentsClient(t *testing.T, window, success string) (*gitlab.Client, *int) {
	t.Helper()
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/amberpixels%2Fp44/deployments", func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if q.Get("environment") != "production" || q.Get("order_by") != "id" || q.Get("sort") != "desc" {
			http.Error(w, "bad query "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		if q.Get("status") == "success" {
			fmt.Fprint(w, success)
			return
		}
		fmt.Fprint(w, window)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return gitlab.NewClient(srv.URL, "amberpixels/p44", ""), &calls
}

func TestDeployments(t *testing.T) {
	row := func(sha, status string) string {
		return fmt.Sprintf(`{"sha": %q, "status": %q, "created_at": "2026-07-13T08:00:00Z"}`, sha, status)
	}
	cases := []struct {
		name, window, success string
		wantSHA               string
		wantRollout           onair.RolloutStatus
		wantRolloutSHA        string
	}{
		{name: "newest is success", window: "[" + row("aaa", "success") + "]", wantSHA: "aaa"},
		{
			name:    "running above success",
			window:  "[" + row("ccc", "running") + "," + row("bbb", "failed") + "," + row("aaa", "success") + "]",
			wantSHA: "aaa", wantRollout: onair.RolloutPending, wantRolloutSHA: "ccc",
		},
		{
			name:    "failed above success",
			window:  "[" + row("bbb", "failed") + "," + row("aaa", "success") + "]",
			wantSHA: "aaa", wantRollout: onair.RolloutFailed, wantRolloutSHA: "bbb",
		},
		{
			name:    "unplayed manual deploy says nothing",
			window:  "[" + row("ccc", "created") + "," + row("ddd", "blocked") + "," + row("aaa", "success") + "]",
			wantSHA: "aaa",
		},
		{
			name:    "canceled says nothing",
			window:  "[" + row("bbb", "canceled") + "," + row("aaa", "success") + "]",
			wantSHA: "aaa",
		},
		{
			name:    "success older than the window",
			window:  "[" + row("bbb", "failed") + "]",
			success: "[" + row("aaa", "success") + "]",
			wantSHA: "aaa", wantRollout: onair.RolloutFailed, wantRolloutSHA: "bbb",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := deploymentsClient(t, tc.window, tc.success)
			info, err := gitlab.Deployments{Client: client, Environment: "production"}.Live(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if info.SHA != tc.wantSHA || info.Confidence != onair.Recorded {
				t.Errorf("got %+v, want recorded %s", info, tc.wantSHA)
			}
			switch {
			case tc.wantRollout == "" && info.Rollout != nil:
				t.Errorf("expected no rollout, got %+v", info.Rollout)
			case tc.wantRollout != "" && (info.Rollout == nil ||
				info.Rollout.Status != tc.wantRollout || info.Rollout.SHA != tc.wantRolloutSHA):
				t.Errorf("got rollout %+v, want %s %s", info.Rollout, tc.wantRollout, tc.wantRolloutSHA)
			}
		})
	}
}

func TestDeploymentsNoneSucceeded(t *testing.T) {
	client, _ := deploymentsClient(t, `[{"sha": "bbb", "status": "running"}]`, "[]")
	_, err := gitlab.Deployments{Client: client, Environment: "production"}.Live(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no successful deployment") {
		t.Errorf("expected a no-success error, got %v", err)
	}
}

func TestDeploymentsSharedPerEnvironment(t *testing.T) {
	client, calls := deploymentsClient(t, `[{"sha": "aaa", "status": "success"}]`, "")
	for range 3 {
		if _, err := (gitlab.Deployments{Client: client, Environment: "production"}).Live(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if *calls != 1 {
		t.Errorf("expected one request per environment, got %d", *calls)
	}
}

// A cancelled first call must not poison the key: the next caller fetches
// again. Concurrent callers of one key share one request.
func TestResolveMemo(t *testing.T) {
	calls := make(chan struct{}, 16)
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/amberpixels%2Fp44/repository/commits/", func(w http.ResponseWriter, r *http.Request) {
		calls <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, commitJSON)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := gitlab.NewClient(srv.URL, "amberpixels/p44", "")

	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-calls; cancel() }()
	if _, err := client.Resolve(ctx, "a1b2c3d"); err == nil {
		t.Fatal("expected the cancelled call to fail")
	}

	close(release)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if ref, err := client.Resolve(context.Background(), "a1b2c3d"); err != nil || ref.Subject == "" {
				t.Errorf("resolve after a cancelled call: %+v, %v", ref, err)
			}
		})
	}
	wg.Wait()
	if n := len(calls); n != 1 {
		t.Errorf("expected one request after the cancelled one, got %d", n)
	}
	// The full SHA was stored with the short one.
	if _, err := client.Resolve(context.Background(), "a1b2c3d000000000000000000000000000000000"); err != nil || len(calls) != 1 {
		t.Errorf("full SHA should hit the memo: %v, %d calls", err, len(calls))
	}
}
