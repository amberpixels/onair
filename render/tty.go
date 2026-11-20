package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amberpixels/onair"
)

// TTYOptions controls the terminal renderer.
type TTYOptions struct {
	Color bool
	// Now anchors the "(2h ago)" humanization; zero means time.Now().
	Now time.Time
}

// styles is the report's look, after Ruby onair's: pink project header,
// one color per tier (HEAD yellow, Green green, Live cyan). Bright ANSI
// colors inherit the user's terminal theme instead of fighting it.
type styles struct {
	project lipgloss.Style
	head    lipgloss.Style
	green   lipgloss.Style
	live    lipgloss.Style
	sha     lipgloss.Style
	dim     lipgloss.Style
	warn    lipgloss.Style
	bad     lipgloss.Style
	good    lipgloss.Style
	mine    lipgloss.Style
}

func newStyles(w io.Writer, color bool) styles {
	ren := lipgloss.NewRenderer(w)
	if color {
		ren.SetColorProfile(termenv.ANSI)
	} else {
		ren.SetColorProfile(termenv.Ascii)
	}
	tier := func(c string) lipgloss.Style {
		return ren.NewStyle().Foreground(lipgloss.Color(c)).Bold(true)
	}
	return styles{
		project: tier("13"), // bright magenta
		head:    tier("11"), // bright yellow
		green:   tier("10"), // bright green
		live:    tier("14"), // bright cyan
		sha:     ren.NewStyle().Bold(true),
		dim:     ren.NewStyle().Faint(true),
		warn:    ren.NewStyle().Foreground(lipgloss.Color("11")),
		bad:     ren.NewStyle().Foreground(lipgloss.Color("9")),
		good:    ren.NewStyle().Foreground(lipgloss.Color("10")),
		mine:    ren.NewStyle().Foreground(lipgloss.Color("14")),
	}
}

func (s styles) label(st lipgloss.Style, name string) string {
	return st.Render(fmt.Sprintf("%-7s", name))
}

// TTY writes the human view of the report. Missing tiers render as honest
// labels, never as errors - degrade gracefully, always show something useful.
func TTY(w io.Writer, r *onair.Report, opts TTYOptions) error {
	p := &printer{w: w, s: newStyles(w, opts.Color), now: opts.Now}
	if p.now.IsZero() {
		p.now = time.Now()
	}

	// Header: project · env                     host/repo
	right := r.Forge.Repo
	if r.Forge.Host != "" {
		right = r.Forge.Host + "/" + r.Forge.Repo
	}
	pad := max(45-(len(r.Project)+3+len(r.Environment)), 2)
	p.linef("%s · %s%s%s", p.s.project.Render(r.Project), r.Environment,
		strings.Repeat(" ", pad), p.s.dim.Render(right))
	p.linef("")

	// The common, boring, good case: everything on air is the latest commit.
	// Collapse the three identical tiers to one "Deployed: … ★ current" line -
	// the multi-tier breakdown is only worth its space when tiers disagree.
	if live, ok := converged(r); ok {
		p.deployedRow(live)
		for _, e := range r.Errors {
			p.linef("")
			p.linef("%s %s", p.s.warn.Render("!"), p.s.dim.Render(e))
		}
		return p.err
	}

	if r.Head != nil {
		p.commitRow(p.s.label(p.s.head, "HEAD"), r.Head, "")
		p.subjectRow(r.Head)
	} else {
		p.linef("%s %s", p.s.label(p.s.head, "HEAD"), p.s.dim.Render("unavailable"))
	}
	p.linef("")

	if r.Green != nil {
		suffix := ""
		if r.Head != nil {
			if onair.SameSHA(r.Green.SHA, r.Head.SHA) {
				suffix = p.s.good.Render("★ == HEAD")
			} else if r.Green.BehindHead > 0 {
				suffix = p.s.warn.Render(fmt.Sprintf("↓ %d behind HEAD", r.Green.BehindHead))
			}
		}
		p.commitRow(p.s.label(p.s.green, "Green"), &r.Green.CommitInfo, suffix)
		p.subjectRow(&r.Green.CommitInfo)
	} else {
		p.linef("%s %s", p.s.label(p.s.green, "Green"),
			p.s.dim.Render("unavailable (no successful pipeline found)"))
	}
	p.linef("")

	p.liveRows(r)

	for _, e := range r.Errors {
		p.linef("")
		p.linef("%s %s", p.s.warn.Render("!"), p.s.dim.Render(e))
	}
	return p.err
}

func (p *printer) liveRows(r *onair.Report) {
	label := p.s.label(p.s.live, "Live")
	leader := leaderComponent(r)
	if leader == nil || leader.Live == nil {
		p.linef("%s %s", label, p.s.dim.Render("unknown"))
		for _, c := range r.Components {
			if c.Error != "" {
				p.linef("  %s %s", c.Name, p.s.dim.Render("("+c.Error+")"))
			}
		}
		return
	}

	lead := leader.Live
	var suffix []string
	if mark := p.stateMark(lead); mark != "" {
		suffix = append(suffix, mark)
	}
	if lead.BehindGreen > 0 {
		suffix = append(suffix, p.s.warn.Render(fmt.Sprintf("↓ %d behind green", lead.BehindGreen)))
	}
	if lead.BehindHead > 0 {
		suffix = append(suffix, p.s.warn.Render(fmt.Sprintf("%d behind %s", lead.BehindHead, r.Forge.Branch)))
	}
	switch lead.Confidence {
	case onair.Assumed:
		suffix = append(suffix, p.s.dim.Render("(assumed = green)"))
	case onair.Reported:
		suffix = append(suffix, p.s.dim.Render("(reported)"))
	case onair.Recorded:
		suffix = append(suffix, p.s.dim.Render("(recorded)"))
	}
	if lead.Mine {
		suffix = append(suffix, p.s.mine.Render("✓ yours"))
	}
	p.commitRow(label, &lead.CommitInfo, strings.Join(suffix, " · "))

	if len(r.Components) > 1 {
		allAssumed := true
		for _, c := range r.Components {
			if c.Live != nil && c.Live.Confidence != onair.Assumed {
				allAssumed = false
			}
		}
		var cells []string
		for _, c := range r.Components {
			if c.Live == nil {
				cells = append(cells, fmt.Sprintf("%s %s", c.Name, p.s.dim.Render("unknown")))
				continue
			}
			cell := fmt.Sprintf("%s %s", c.Name, p.s.sha.Render(onair.ShortSHA(c.Live.SHA)))
			if mark := p.stateMark(c.Live); mark != "" {
				cell += " " + mark
			}
			switch {
			case c.Live.Confidence == onair.Assumed && !allAssumed:
				cell += " " + p.s.dim.Render("(assumed)")
			case c.Live.Confidence.Weaker(lead.Confidence):
				// The Live row labels only the leader, so a weaker claim
				// beside it carries its own label.
				cell += " " + p.s.dim.Render("("+string(c.Live.Confidence)+")")
			}
			cells = append(cells, cell)
		}
		// "in sync" speaks for the whole deploy, so it needs every component
		// on one commit and serving it.
		var status []string
		if r.Drift != nil {
			status = append(status, p.s.warn.Render("⚠ "+r.Drift.Message))
		}
		var down []string
		for _, c := range r.Components {
			if !c.Live.Serving() {
				down = append(down, c.Name)
			}
		}
		if len(down) > 0 {
			status = append(status, p.s.bad.Render("✗ "+strings.Join(down, ", ")+" not serving"))
		}
		if len(status) == 0 {
			status = append(status, p.s.good.Render("in sync"))
		}
		p.linef("  %s   %s", strings.Join(cells, " · "), strings.Join(status, " · "))
	}
	p.subjectRow(&lead.CommitInfo)
	p.rolloutRows(r)
}

// rolloutRows prints one line per component whose newer deploy is in flight
// or failed, named when there are several components.
func (p *printer) rolloutRows(r *onair.Report) {
	for _, c := range r.Components {
		if c.Live == nil || c.Live.Rollout == nil {
			continue
		}
		ro := c.Live.Rollout
		sha := p.s.sha.Render(onair.ShortSHA(ro.SHA))
		var line string
		switch ro.Status {
		case onair.RolloutPending:
			line = p.s.warn.Render("⟳ deploying") + " " + sha
		case onair.RolloutFailed:
			line = p.s.warn.Render("✗ deploy of") + " " + sha + " " + p.s.warn.Render("failed")
		default:
			line = string(ro.Status) + " " + sha
		}
		if !ro.At.IsZero() {
			line += " " + p.s.dim.Render("("+ago(p.now.Sub(ro.At))+")")
		}
		if len(r.Components) > 1 {
			line = c.Name + " " + line
		}
		p.linef("  %s", line)
	}
}

// converged reports the single deployed commit when every known tier agrees on
// it - HEAD == Green == every component's Live, with no cross-component drift
// and every component serving (a restart loop on HEAD is not "current") and
// no deploy in flight or failed (the collapsed line would hide it).
// That is the signal to collapse the report to one line. It returns the Live
// with the weakest confidence (they all share the SHA), so the one line never
// claims more than the least certain component knows.
func converged(r *onair.Report) (*onair.LiveReport, bool) {
	if r.Head == nil || r.Green == nil || r.Drift != nil {
		return nil, false
	}
	if !onair.SameSHA(r.Green.SHA, r.Head.SHA) {
		return nil, false
	}
	if len(r.Components) == 0 {
		return nil, false
	}
	var leader *onair.LiveReport
	for i := range r.Components {
		c := &r.Components[i]
		if c.Live == nil || c.Live.Rollout != nil || !onair.SameSHA(c.Live.SHA, r.Head.SHA) || !c.Live.Serving() {
			return nil, false
		}
		if leader == nil || c.Live.Confidence.Weaker(leader.Confidence) {
			leader = c.Live
		}
	}
	return leader, leader != nil
}

// deployedRow is the collapsed, converged view: one line saying the latest
// commit is what's on air. When Live is only assumed from Green it still says
// so - honesty over a cleaner line.
func (p *printer) deployedRow(live *onair.LiveReport) {
	suffix := []string{p.s.good.Render("★ current")}
	switch live.Confidence {
	case onair.Assumed:
		suffix = append(suffix, p.s.dim.Render("(assumed = green)"))
	case onair.Recorded:
		suffix = append(suffix, p.s.dim.Render("(recorded)"))
	}
	if live.Mine {
		suffix = append(suffix, p.s.mine.Render("✓ yours"))
	}
	p.commitRow(p.s.green.Render("Deployed:"), &live.CommitInfo, strings.Join(suffix, " · "))
	p.subjectRow(&live.CommitInfo)
}

// stateMark flags a component that is not serving with its reported state,
// e.g. "✗ restarting"; serving or unknown states get no mark.
func (p *printer) stateMark(l *onair.LiveReport) string {
	if l.Serving() {
		return ""
	}
	return p.s.bad.Render("✗ " + l.State)
}

// commitRow prints a tier's main line: label, sha, age, author, suffix.
func (p *printer) commitRow(label string, ci *onair.CommitInfo, suffix string) {
	parts := []string{label, p.s.sha.Render(onair.ShortSHA(ci.SHA))}
	if !ci.At.IsZero() {
		parts = append(parts, p.s.dim.Render("("+ago(p.now.Sub(ci.At))+")"))
	}
	if ci.Author != "" {
		parts = append(parts, "by "+ci.Author)
	}
	if suffix != "" {
		parts = append(parts, " "+suffix)
	}
	p.linef("%s", strings.Join(parts, " "))
}

func (p *printer) subjectRow(ci *onair.CommitInfo) {
	if ci.Subject == "" {
		return
	}
	line := "→ " + ci.Subject
	if ci.Request != "" {
		line += p.s.dim.Render(" • ↗ ") + ci.Request
	}
	if ci.TaskID != "" {
		line += p.s.dim.Render(" • ⧉ ") + ci.TaskID
	}
	p.linef("%s", line)
}

// leaderComponent picks the component whose Live leads - the drift leader
// when there is drift, otherwise the first component that has a Live at all.
func leaderComponent(r *onair.Report) *onair.ComponentReport {
	if r.Drift != nil {
		for i := range r.Components {
			if r.Components[i].Name == r.Drift.Leader {
				return &r.Components[i]
			}
		}
	}
	for i := range r.Components {
		if r.Components[i].Live != nil {
			return &r.Components[i]
		}
	}
	return nil
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// printer accumulates the first write error so callers check once.
type printer struct {
	w   io.Writer
	s   styles
	now time.Time
	err error
}

// gutter is the left padding on every non-blank line, so the report breathes
// instead of hugging the terminal's left edge.
const gutter = "  "

func (p *printer) linef(format string, args ...any) {
	if p.err != nil {
		return
	}
	prefix := gutter
	if format == "" {
		prefix = "" // blank separators stay empty, no trailing spaces
	}
	_, p.err = fmt.Fprintf(p.w, prefix+format+"\n", args...)
}
