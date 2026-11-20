package main

import (
	"strings"
	"testing"

	"github.com/amberpixels/onair"
	"github.com/amberpixels/onair/config"
	"github.com/amberpixels/onair/gitlab"
	"github.com/amberpixels/onair/live"
)

func TestBuildComponentsPrecedence(t *testing.T) {
	forge := gitlab.NewClient("", "acme/shop", "")
	env := config.Environment{
		Environment: "production",
		Components: []config.Component{
			{Name: "bare"},
			{Name: "own-env", Live: &config.LiveSource{Environment: "production-web"}},
			{Name: "probed", Live: &config.LiveSource{Probe: "https://example.test/-/version"}},
			{Name: "opted-out", Live: &config.LiveSource{}},
			{Name: "piped"},
		},
	}

	comps, err := buildComponents(env, forge, "a1b2c3d", "piped")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]onair.LiveProvider{}
	for _, c := range comps {
		byName[c.Name] = c.Live
	}

	if d, ok := byName["bare"].(gitlab.Deployments); !ok || d.Environment != "production" {
		t.Errorf("bare component should inherit the environment's records, got %#v", byName["bare"])
	}
	if d, ok := byName["own-env"].(gitlab.Deployments); !ok || d.Environment != "production-web" {
		t.Errorf("a component's own environment should win, got %#v", byName["own-env"])
	}
	if _, ok := byName["probed"].(*live.Probe); !ok {
		t.Errorf("a probe should win over the environment's records, got %#v", byName["probed"])
	}
	if byName["opted-out"] != nil {
		t.Errorf("an empty live block should stay assumed, got %#v", byName["opted-out"])
	}
	if s, ok := byName["piped"].(live.Static); !ok || s.Info.SHA != "a1b2c3d" {
		t.Errorf("-live-ref should win over everything, got %#v", byName["piped"])
	}
}

func TestBuildComponentsRejectsUnusedComponent(t *testing.T) {
	forge := gitlab.NewClient("", "acme/shop", "")
	env := config.Environment{Components: []config.Component{{Name: "backend"}, {Name: "web"}}}

	_, err := buildComponents(env, forge, "a1b2c3d", "backnd")
	if err == nil || !strings.Contains(err.Error(), `"backnd"`) || !strings.Contains(err.Error(), "backend, web") {
		t.Errorf("an unknown -component should fail naming the configured ones, got %v", err)
	}
	if _, err := buildComponents(env, forge, "", "web"); err == nil {
		t.Error("-component without -live-ref should fail")
	}
}
