package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amberpixels/onair"
	"github.com/amberpixels/onair/config"
)

const sample = `
project: p44
forge: { kind: gitlab, repo: amberpixels/p44, branch: main }
attribution: auto
identities:
  eugene: [amber.pixels.io@gmail.com]
task: { pattern: 'WS-\d+', url: 'https://tasks.test/{id}' }
environments:
  prod:
    components:
      - name: backend
        live: { probe: "https://pieton.md/api/-/version" }
      - name: web
        live: { command: "ssh prod docker inspect web --format '{{.Config.Image}}'" }
      - name: worker
`

func write(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, config.DefaultFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	f, err := config.Load(write(t, t.TempDir(), sample))
	if err != nil {
		t.Fatal(err)
	}
	if f.Forge.TokenEnv != "GITLAB_TOKEN" {
		t.Errorf("token_env default: got %q", f.Forge.TokenEnv)
	}
	comps := f.Environments["prod"].Components
	if len(comps) != 3 || comps[1].Live.Command == "" || comps[2].Live != nil {
		t.Errorf("components parsed wrong: %+v", comps)
	}
	// onair.TaskConfig carries no yaml tags; its lowercased names must decode.
	if f.Task == nil || f.Task.Pattern != `WS-\d+` || f.Task.URL != "https://tasks.test/{id}" {
		t.Errorf("task parsed wrong: %+v", f.Task)
	}
}

// The example shipped beside live.sh must keep loading as the schema changes.
func TestLoadComposeLiveExample(t *testing.T) {
	f, err := config.Load(filepath.Join("..", "contrib", "compose-live", "example.onair.yml"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"web":    "ssh prod onair-live p44 web",
		"worker": "ssh prod onair-live p44 worker",
	}
	for _, c := range f.Environments["prod"].Components {
		if w, ok := want[c.Name]; ok {
			if c.Live == nil || c.Live.Command != w {
				t.Errorf("%s: live = %+v, want command %q", c.Name, c.Live, w)
			}
			delete(want, c.Name)
		}
	}
	if len(want) > 0 {
		t.Errorf("components missing from the example: %v", want)
	}
}

func TestLoadDefaults(t *testing.T) {
	f, err := config.Load(write(t, t.TempDir(), `
forge: { repo: amberpixels/p44 }
environments:
  prod:
    components: [{ name: app }]
`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Project != "p44" || f.Forge.Kind != "gitlab" || f.Attribution != onair.AttributionAuto {
		t.Errorf("defaults not applied: %+v", f)
	}
	// The branch default belongs to onair.Collect, so config leaves it empty.
	if f.Forge.Branch != "" {
		t.Errorf("branch should stay empty for Collect to default, got %q", f.Forge.Branch)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]string{
		"missing repo":      "environments: { prod: { components: [{ name: a }] } }",
		"unknown forge":     "forge: { kind: github, repo: a/b }\nenvironments: { prod: { components: [{ name: a }] } }",
		"no environments":   "forge: { repo: a/b }",
		"nameless comp":     "forge: { repo: a/b }\nenvironments: { prod: { components: [{ live: { probe: x } }] } }",
		"probe AND command": "forge: { repo: a/b }\nenvironments: { prod: { components: [{ name: a, live: { probe: x, command: y } }] } }",
		"probe AND env":     "forge: { repo: a/b }\nenvironments: { prod: { components: [{ name: a, live: { probe: x, environment: y } }] } }",
		"bad attribution":   "forge: { repo: a/b }\nattribution: maybe\nenvironments: { prod: { components: [{ name: a }] } }",
		"unknown key":       "forge: { repo: a/b }\ntypo_key: 1\nenvironments: { prod: { components: [{ name: a }] } }",
		"bare live":         "forge: { repo: a/b }\nenvironments: { prod: { components: [{ name: a, live: }] } }",
		"null live":         "forge: { repo: a/b }\nenvironments: { prod: { components: [{ name: a, live: null }] } }",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Load(write(t, t.TempDir(), content)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestLoadDeploymentEnvironments(t *testing.T) {
	f, err := config.Load(write(t, t.TempDir(), `
forge: { repo: amberpixels/p44 }
environments:
  prod:
    environment: production
    components:
      - name: backend
      - name: web
        live: { environment: production-web }
`))
	if err != nil {
		t.Fatal(err)
	}
	env := f.Environments["prod"]
	if env.Environment != "production" || env.Components[1].Live.Environment != "production-web" {
		t.Errorf("deployment environments parsed wrong: %+v", env)
	}
}

func TestFindWalksUp(t *testing.T) {
	root := t.TempDir()
	write(t, root, sample)
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := config.Find(nested)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, config.DefaultFileName) {
		t.Errorf("got %s", path)
	}
}
