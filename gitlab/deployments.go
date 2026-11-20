package gitlab

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/amberpixels/onair"
)

// deploymentWindow is how many recent deployments are scanned for the newest
// success and any newer in-flight or failed deploy above it.
const deploymentWindow = 20

// Deployments is an onair.LiveProvider that reads the deployments GitLab
// records for an environment: the newest successful one is Live, with
// Recorded confidence. It reports what CI recorded at deploy time, not what
// the host runs now.
type Deployments struct {
	Client      *Client
	Environment string // GitLab environment name, e.g. "production"
}

var _ onair.LiveProvider = Deployments{}

func (d Deployments) Live(ctx context.Context) (onair.LiveInfo, error) {
	return d.Client.deploymentLive(ctx, d.Environment)
}

type glDeployment struct {
	SHA       string    `json:"sha"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// deploymentsResult caches one environment's answer. Components often share a
// deploy environment, so concurrent callers wait on one request.
type deploymentsResult struct {
	once sync.Once
	info onair.LiveInfo
	err  error
}

func (c *Client) deploymentLive(ctx context.Context, env string) (onair.LiveInfo, error) {
	c.mu.Lock()
	res, ok := c.deployments[env]
	if !ok {
		res = &deploymentsResult{}
		c.deployments[env] = res
	}
	c.mu.Unlock()
	res.once.Do(func() { res.info, res.err = c.fetchDeploymentLive(ctx, env) })
	return res.info, res.err
}

func (c *Client) fetchDeploymentLive(ctx context.Context, env string) (onair.LiveInfo, error) {
	rows, err := c.listDeployments(ctx, env, "", deploymentWindow)
	if err != nil {
		return onair.LiveInfo{}, err
	}

	var pending, failed *onair.Rollout
	for _, row := range rows {
		switch row.Status {
		case "success":
			return recorded(row.SHA, pending, failed), nil
		case "running":
			if pending == nil {
				pending = &onair.Rollout{SHA: row.SHA, Status: onair.RolloutPending, At: row.CreatedAt}
			}
		case "failed":
			if failed == nil {
				failed = &onair.Rollout{SHA: row.SHA, Status: onair.RolloutFailed, At: row.CreatedAt}
			}
		}
		// canceled and skipped deploys never touched the host, and created or
		// blocked ones (an unplayed manual job, a pending approval) may sit for
		// days without starting: none of them is a deploy in flight.
	}

	// No success in the window: the last good deploy is older than it.
	older, err := c.listDeployments(ctx, env, "success", 1)
	if err != nil {
		return onair.LiveInfo{}, err
	}
	if len(older) == 0 {
		return onair.LiveInfo{}, fmt.Errorf("no successful deployment in environment %q", env)
	}
	return recorded(older[0].SHA, pending, failed), nil
}

// recorded is Live from a successful deployment. A newer deploy still in
// flight outranks a newer failed one, since it decides what runs next.
func recorded(sha string, pending, failed *onair.Rollout) onair.LiveInfo {
	info := onair.LiveInfo{SHA: sha, Confidence: onair.Recorded, Rollout: pending}
	if info.Rollout == nil {
		info.Rollout = failed
	}
	return info
}

// listDeployments returns the environment's deployments, newest first.
func (c *Client) listDeployments(ctx context.Context, env, status string, limit int) ([]glDeployment, error) {
	q := url.Values{
		"environment": {env},
		"order_by":    {"id"},
		"sort":        {"desc"},
		"per_page":    {fmt.Sprint(limit)},
	}
	if status != "" {
		q.Set("status", status)
	}
	var rows []glDeployment
	if err := c.get(ctx, "deployments", q, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
