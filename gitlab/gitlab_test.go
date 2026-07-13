package gitlab_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
