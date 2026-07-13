// Package gitlab implements the onair.Forge seam against the GitLab REST
// API. It is the first Forge, not the architecture - GitHub is a later
// drop-in behind the same interface.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/amberpixels/onair"
)

// DefaultBaseURL is used when no self-hosted instance is configured.
const DefaultBaseURL = "https://gitlab.com"

// Client is an onair.Forge backed by one GitLab project. The zero value is
// not usable; construct with NewClient.
type Client struct {
	baseURL string
	repo    string // "group/project"
	token   string
	http    *http.Client

	mu      sync.Mutex
	commits map[string]onair.Ref // Resolve cache
	behinds map[string]int       // Behind cache, key "from..to"
}

// NewClient returns a Forge for repo ("group/project") on baseURL (empty
// means gitlab.com). An empty token works for public projects.
func NewClient(baseURL, repo, token string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		repo:    repo,
		token:   token,
		http:    &http.Client{Timeout: 15 * time.Second},
		commits: map[string]onair.Ref{},
		behinds: map[string]int{},
	}
}

// Host returns the instance host for display (e.g. "gitlab.com").
func (c *Client) Host() string {
	if u, err := url.Parse(c.baseURL); err == nil && u.Host != "" {
		return u.Host
	}
	return c.baseURL
}

var _ onair.Forge = (*Client)(nil)

type glCommit struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	AuthorName  string    `json:"author_name"`
	AuthorEmail string    `json:"author_email"`
	CreatedAt   time.Time `json:"created_at"`
	WebURL      string    `json:"web_url"`
}

func (g glCommit) ref() onair.Ref {
	return onair.Ref{
		SHA:         g.ID,
		Subject:     g.Title,
		AuthorName:  g.AuthorName,
		AuthorEmail: g.AuthorEmail,
		At:          g.CreatedAt,
		URL:         g.WebURL,
	}
}

func (c *Client) Head(ctx context.Context, branch string) (onair.Ref, error) {
	var commits []glCommit
	err := c.get(ctx, "repository/commits", url.Values{"ref_name": {branch}, "per_page": {"1"}}, &commits)
	if err != nil {
		return onair.Ref{}, err
	}
	if len(commits) == 0 {
		return onair.Ref{}, fmt.Errorf("no commits on %q", branch)
	}
	return commits[0].ref(), nil
}

func (c *Client) LatestGreen(ctx context.Context, branch string) (onair.Ref, error) {
	var pipelines []struct {
		SHA string `json:"sha"`
	}
	err := c.get(ctx, "pipelines", url.Values{
		"ref": {branch}, "status": {"success"}, "per_page": {"1"},
	}, &pipelines)
	if err != nil {
		return onair.Ref{}, err
	}
	if len(pipelines) == 0 {
		return onair.Ref{}, fmt.Errorf("no successful pipeline on %q", branch)
	}
	return c.Resolve(ctx, pipelines[0].SHA)
}

func (c *Client) Resolve(ctx context.Context, sha string) (onair.Ref, error) {
	c.mu.Lock()
	if ref, ok := c.commits[sha]; ok {
		c.mu.Unlock()
		return ref, nil
	}
	c.mu.Unlock()

	var commit glCommit
	if err := c.get(ctx, "repository/commits/"+url.PathEscape(sha), nil, &commit); err != nil {
		return onair.Ref{}, err
	}
	ref := commit.ref()
	c.mu.Lock()
	c.commits[sha], c.commits[ref.SHA] = ref, ref
	c.mu.Unlock()
	return ref, nil
}

func (c *Client) Behind(ctx context.Context, from, to string) (int, error) {
	key := from + ".." + to
	c.mu.Lock()
	if n, ok := c.behinds[key]; ok {
		c.mu.Unlock()
		return n, nil
	}
	c.mu.Unlock()

	var cmp struct {
		Commits []struct{} `json:"commits"`
	}
	err := c.get(ctx, "repository/compare", url.Values{"from": {from}, "to": {to}}, &cmp)
	if err != nil {
		return 0, err
	}
	n := len(cmp.Commits)
	c.mu.Lock()
	c.behinds[key] = n
	c.mu.Unlock()
	return n, nil
}

func (c *Client) Recent(ctx context.Context, branch string, limit int) ([]onair.Ref, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var commits []glCommit
	err := c.get(ctx, "repository/commits", url.Values{
		"ref_name": {branch}, "per_page": {fmt.Sprint(limit)},
	}, &commits)
	if err != nil {
		return nil, err
	}
	refs := make([]onair.Ref, len(commits))
	for i, g := range commits {
		refs[i] = g.ref()
	}
	return refs, nil
}

func (c *Client) CommitURL(sha string) string {
	return fmt.Sprintf("%s/%s/-/commit/%s", c.baseURL, c.repo, sha)
}

func (c *Client) RequestURL(id string) string {
	return fmt.Sprintf("%s/%s/-/merge_requests/%s", c.baseURL, c.repo, id)
}

// get performs one API call against the project and decodes the JSON reply.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := fmt.Sprintf("%s/api/v4/projects/%s/%s", c.baseURL, url.PathEscape(c.repo), path)
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("gitlab: %w", err)
	}
	if c.token != "" {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gitlab: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("gitlab: GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("gitlab: GET %s: bad response: %w", path, err)
	}
	return nil
}
