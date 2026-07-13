package config_test

import (
	"os"
	"path/filepath"
	"testing"

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
	if f.Project != "p44" || f.Forge.Kind != "gitlab" || f.Forge.Branch != "main" || f.Attribution != "auto" {
		t.Errorf("defaults not applied: %+v", f)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]string{
		"missing repo":      "environments: { prod: { components: [{ name: a }] } }",
		"unknown forge":     "forge: { kind: github, repo: a/b }\nenvironments: { prod: { components: [{ name: a }] } }",
		"no environments":   "forge: { repo: a/b }",
		"nameless comp":     "forge: { repo: a/b }\nenvironments: { prod: { components: [{ live: { probe: x } }] } }",
		"probe AND command": "forge: { repo: a/b }\nenvironments: { prod: { components: [{ name: a, live: { probe: x, command: y } }] } }",
		"bad attribution":   "forge: { repo: a/b }\nattribution: maybe\nenvironments: { prod: { components: [{ name: a }] } }",
		"unknown key":       "forge: { repo: a/b }\ntypo_key: 1\nenvironments: { prod: { components: [{ name: a }] } }",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Load(write(t, t.TempDir(), content)); err == nil {
				t.Error("expected an error")
			}
		})
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
