// Command onair answers "which commit is actually running" for the project
// described by .onair.yml. It is a thin consumer of the core: load config,
// wire providers, Collect, render.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/amberpixels/onair"
	"github.com/amberpixels/onair/config"
	"github.com/amberpixels/onair/gitlab"
	"github.com/amberpixels/onair/live"
	"github.com/amberpixels/onair/render"
)

var version = "dev" // set via -ldflags at release time

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "onair:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath  = flag.String("config", "", "path to .onair.yml (default: found upward from cwd)")
		env         = flag.String("env", "", "environment to show (default: the only one configured)")
		branch      = flag.String("branch", "", "override the tracked branch")
		jsonOut     = flag.Bool("json", false, "emit the Report as JSON")
		noColor     = flag.Bool("no-color", false, "disable colors")
		liveRef     = flag.String("live-ref", "", `live ref override: a commit-ish, or "-" to read one from stdin`)
		component   = flag.String("component", "", "component the -live-ref applies to (default: the only one)")
		timeout     = flag.Duration("timeout", 15*time.Second, "overall deadline")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("onair", version)
		return nil
	}

	path := *configPath
	if path == "" {
		var err error
		if path, err = config.Find("."); err != nil {
			return err
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	envName, envCfg, err := pickEnvironment(cfg, *env)
	if err != nil {
		return err
	}
	components, err := buildComponents(envCfg, *liveRef, *component)
	if err != nil {
		return err
	}

	trackedBranch := *branch
	if trackedBranch == "" {
		trackedBranch = cfg.Forge.Branch
	}
	forge := gitlab.NewClient(cfg.Forge.BaseURL, cfg.Forge.Repo, os.Getenv(cfg.Forge.TokenEnv))

	params := onair.Params{
		Project:     cfg.Project,
		Environment: envName,
		Forge:       forge,
		ForgeInfo: onair.ForgeInfo{
			Kind:   cfg.Forge.Kind,
			Host:   forge.Host(),
			Repo:   cfg.Forge.Repo,
			Branch: trackedBranch,
		},
		Branch:      trackedBranch,
		Components:  components,
		Identity:    gitIdentity{},
		Attribution: onair.AttributionMode(cfg.Attribution),
		Identities:  cfg.Identities,
	}
	if cfg.Task != nil {
		params.Task = &onair.TaskConfig{Pattern: cfg.Task.Pattern, URL: cfg.Task.URL}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := onair.Collect(ctx, params)
	if err != nil {
		return err
	}

	if *jsonOut {
		return render.JSON(os.Stdout, report)
	}
	return render.TTY(os.Stdout, report, render.TTYOptions{Color: useColor(*noColor)})
}

// pickEnvironment applies the one-environment-per-invocation rule.
func pickEnvironment(cfg *config.File, name string) (string, config.Environment, error) {
	if name == "" {
		if len(cfg.Environments) == 1 {
			for n, e := range cfg.Environments {
				return n, e, nil
			}
		}
		var names []string
		for n := range cfg.Environments {
			names = append(names, n)
		}
		sort.Strings(names)
		return "", config.Environment{}, fmt.Errorf("-env is required; configured: %s", strings.Join(names, ", "))
	}
	envCfg, ok := cfg.Environments[name]
	if !ok {
		return "", config.Environment{}, fmt.Errorf("environment %q is not configured", name)
	}
	return name, envCfg, nil
}

// buildComponents wires each configured component to its provider, applying
// the -live-ref override (the pipe form of the command contract).
func buildComponents(env config.Environment, liveRef, componentName string) ([]onair.Component, error) {
	override, overrideTarget := onair.LiveInfo{}, ""
	if liveRef != "" {
		raw := liveRef
		if raw == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return nil, fmt.Errorf("reading -live-ref from stdin: %w", err)
			}
			raw = string(data)
		}
		info, err := live.ParseOutput(raw)
		if err != nil {
			return nil, err
		}
		override = info
		overrideTarget = componentName
		if overrideTarget == "" {
			if len(env.Components) > 1 {
				return nil, fmt.Errorf("-live-ref with %d components needs -component", len(env.Components))
			}
			overrideTarget = env.Components[0].Name
		}
	}

	components := make([]onair.Component, 0, len(env.Components))
	for _, c := range env.Components {
		comp := onair.Component{Name: c.Name}
		switch {
		case c.Name == overrideTarget && liveRef != "":
			comp.Live = live.Static{Info: override}
		case c.Live != nil && c.Live.Probe != "":
			comp.Live = &live.Probe{URL: c.Live.Probe}
		case c.Live != nil && c.Live.Command != "":
			comp.Live = &live.Command{Command: c.Live.Command}
		}
		components = append(components, comp)
	}
	return components, nil
}

// gitIdentity is the CLI's Identity: whoever git says you are.
type gitIdentity struct{}

func (gitIdentity) ViewerEmails(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "config", "user.email").Output()
	if err != nil {
		return nil, nil // not in a repo / no git: attribution stays silent
	}
	email := strings.TrimSpace(string(out))
	if email == "" {
		return nil, nil
	}
	return []string{email}, nil
}

func useColor(noColorFlag bool) bool {
	if noColorFlag || os.Getenv("NO_COLOR") != "" {
		return false
	}
	stat, err := os.Stdout.Stat()
	return err == nil && stat.Mode()&os.ModeCharDevice != 0
}
