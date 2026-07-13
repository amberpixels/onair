package onair

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Params configures a single Collect run: one project, one environment.
type Params struct {
	Project     string
	Environment string
	Forge       Forge
	ForgeInfo   ForgeInfo
	Branch      string // default "main"
	Components  []Component
	Identity    Identity        // nil -> no attribution
	Attribution AttributionMode // default AttributionAuto
	// Identities is the alias map: one person, many emails (work vs
	// personal, forge noreply). Any match pulls in the whole group.
	Identities map[string][]string
	Task       *TaskConfig
	// RecentLimit is how deep the attribution auto-gate looks for authors;
	// default 50.
	RecentLimit int
}

// Collect asks the seams their questions and applies the rules: tier
// comparison, assumed-fallback, drift, attribution. It fails only on
// unusable Params; anything the world refuses to answer becomes a labeled
// degradation inside the Report instead.
func Collect(ctx context.Context, p Params) (*Report, error) {
	if p.Forge == nil {
		return nil, errors.New("onair: Params.Forge is required")
	}
	if p.Branch == "" {
		p.Branch = "main"
	}
	if p.Attribution == "" {
		p.Attribution = AttributionAuto
	}
	if p.RecentLimit <= 0 {
		p.RecentLimit = 50
	}
	taskRe, err := p.Task.compile()
	if err != nil {
		return nil, fmt.Errorf("onair: %w", err)
	}

	r := &Report{
		Project:     p.Project,
		Environment: p.Environment,
		Forge:       p.ForgeInfo,
		Attribution: p.Attribution,
	}
	if r.Forge.Branch == "" {
		r.Forge.Branch = p.Branch
	}

	var head, green Ref
	if head, err = p.Forge.Head(ctx, p.Branch); err != nil {
		r.Errors = append(r.Errors, "head: "+err.Error())
	} else {
		ci := commitInfo(head)
		r.Head = &ci
	}
	if green, err = p.Forge.LatestGreen(ctx, p.Branch); err != nil {
		r.Errors = append(r.Errors, "green: "+err.Error())
	} else {
		r.Green = &GreenInfo{CommitInfo: commitInfo(green), Pipeline: "success"}
		if !head.IsZero() && !SameSHA(green.SHA, head.SHA) {
			r.Green.BehindHead = p.behind(ctx, r, green.SHA, head.SHA)
		}
	}

	viewer := p.viewerEmails(ctx, r)

	r.Components = make([]ComponentReport, len(p.Components))
	var wg sync.WaitGroup
	for i, c := range p.Components {
		wg.Go(func() {
			r.Components[i] = p.collectComponent(ctx, c, head, green)
		})
	}
	wg.Wait()

	r.Drift = p.drift(ctx, r)

	annotate := func(ci *CommitInfo) {
		if ci == nil {
			return
		}
		annotateLinks(ci, p.Forge, taskRe, p.Task)
		ci.Mine = viewer[strings.ToLower(ci.AuthorEmail)]
	}
	annotate(r.Head)
	if r.Green != nil {
		annotate(&r.Green.CommitInfo)
	}
	for i := range r.Components {
		if r.Components[i].Live != nil {
			annotate(&r.Components[i].Live.CommitInfo)
		}
	}
	return r, nil
}

// collectComponent establishes one component's Live tier: ask the provider,
// degrade to Assumed = Green when it cannot answer, resolve metadata, measure
// the lag.
func (p Params) collectComponent(ctx context.Context, c Component, head, green Ref) ComponentReport {
	cr := ComponentReport{Name: c.Name}

	var info LiveInfo
	if c.Live == nil {
		info.Confidence = Assumed
	} else {
		var err error
		if info, err = c.Live.Live(ctx); err != nil {
			cr.Error = err.Error()
			info = LiveInfo{Confidence: Assumed}
		}
	}
	if info.SHA == "" || info.Confidence == Assumed {
		if green.IsZero() {
			if cr.Error == "" {
				cr.Error = "live unknown: no provider and no green pipeline to assume"
			}
			return cr
		}
		info.SHA, info.Confidence = green.SHA, Assumed
	}
	if info.Confidence == "" {
		info.Confidence = Reported
	}

	ref, err := p.Forge.Resolve(ctx, info.SHA)
	if err != nil {
		// A bare SHA is still a report; just less to say about it.
		ref = Ref{SHA: info.SHA}
	}
	lr := &LiveReport{CommitInfo: commitInfo(ref), Confidence: info.Confidence, BuiltAt: info.BuiltAt}
	if !green.IsZero() && !SameSHA(ref.SHA, green.SHA) {
		if n, err := p.Forge.Behind(ctx, ref.SHA, green.SHA); err == nil {
			lr.BehindGreen = n
		}
	}
	if !head.IsZero() && !SameSHA(ref.SHA, head.SHA) {
		if n, err := p.Forge.Behind(ctx, ref.SHA, head.SHA); err == nil {
			lr.BehindHead = n
		}
	}
	cr.Live = lr
	return cr
}

// drift compares Live across components: more than one distinct commit means
// something shipped partially. The most-ahead component leads; the rest are
// measured against it.
func (p Params) drift(ctx context.Context, r *Report) *Drift {
	type entry struct {
		name string
		lr   *LiveReport
	}
	var live []entry
	shas := map[string]bool{}
	for _, c := range r.Components {
		if c.Live == nil {
			continue
		}
		live = append(live, entry{c.Name, c.Live})
		shas[ShortSHA(strings.ToLower(c.Live.SHA))] = true
	}
	if len(shas) <= 1 {
		return nil
	}

	leader := live[0]
	for _, e := range live[1:] {
		if e.lr.At.After(leader.lr.At) {
			leader = e
		}
	}

	var parts []string
	seen := map[string]bool{}
	for _, e := range live {
		if e.name == leader.name || SameSHA(e.lr.SHA, leader.lr.SHA) {
			continue
		}
		key := ShortSHA(strings.ToLower(e.lr.SHA))
		if seen[key] {
			continue
		}
		seen[key] = true
		if n, err := p.Forge.Behind(ctx, e.lr.SHA, leader.lr.SHA); err == nil && n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d behind %s", e.name, n, leader.name))
		} else {
			parts = append(parts, fmt.Sprintf("%s differs from %s", e.name, leader.name))
		}
	}
	sort.Strings(parts)
	return &Drift{Leader: leader.name, Message: strings.Join(parts, " · ")}
}

// viewerEmails resolves who is viewing, expanded through the alias map and -
// in auto mode - gated on the viewer actually being a recent git author.
// Returns the empty set when attribution should stay silent.
func (p Params) viewerEmails(ctx context.Context, r *Report) map[string]bool {
	if p.Attribution == AttributionOff || p.Identity == nil {
		return nil
	}
	emails, err := p.Identity.ViewerEmails(ctx)
	if err != nil {
		r.Errors = append(r.Errors, "identity: "+err.Error())
		return nil
	}
	viewer := map[string]bool{}
	for _, e := range emails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			viewer[e] = true
		}
	}
	for _, group := range p.Identities {
		match := false
		for _, e := range group {
			if viewer[strings.ToLower(e)] {
				match = true
				break
			}
		}
		if match {
			for _, e := range group {
				viewer[strings.ToLower(e)] = true
			}
		}
	}
	if len(viewer) == 0 {
		return nil
	}
	if p.Attribution == AttributionAuto {
		recent, err := p.Forge.Recent(ctx, p.Branch, p.RecentLimit)
		if err != nil {
			r.Errors = append(r.Errors, "attribution gate: "+err.Error())
			return nil
		}
		gate := false
		for _, ref := range recent {
			if viewer[strings.ToLower(ref.AuthorEmail)] {
				gate = true
				break
			}
		}
		if !gate {
			return nil
		}
	}
	return viewer
}

// behind measures from..to, degrading to 0 with a note instead of failing.
func (p Params) behind(ctx context.Context, r *Report, from, to string) int {
	n, err := p.Forge.Behind(ctx, from, to)
	if err != nil {
		r.Errors = append(r.Errors, "behind: "+err.Error())
		return 0
	}
	return n
}

func commitInfo(ref Ref) CommitInfo {
	return CommitInfo{
		SHA:         ref.SHA,
		Subject:     ref.Subject,
		Author:      ref.AuthorName,
		AuthorEmail: ref.AuthorEmail,
		At:          ref.At,
		URL:         ref.URL,
	}
}
