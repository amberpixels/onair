// Package config loads and validates .onair.yml. The same shape can be built
// programmatically by a host - this package exists only for the CLI's
// zero-code path.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultFileName is what Find looks for, walking up from the working
// directory the way git finds its root.
const DefaultFileName = ".onair.yml"

// File is the parsed .onair.yml.
type File struct {
	Project      string                 `yaml:"project"`
	Forge        Forge                  `yaml:"forge"`
	Attribution  string                 `yaml:"attribution"` // off | on | auto (default auto)
	Identities   map[string][]string    `yaml:"identities"`
	Task         *Task                  `yaml:"task"`
	Environments map[string]Environment `yaml:"environments"`
}

// Forge names the git host and project.
type Forge struct {
	Kind    string `yaml:"kind"` // default "gitlab"
	Repo    string `yaml:"repo"`
	Branch  string `yaml:"branch"`   // default "main"
	BaseURL string `yaml:"base_url"` // default per kind (gitlab.com)
	// TokenEnv names the environment variable holding the API token;
	// default "GITLAB_TOKEN". The token itself never lives in the file.
	TokenEnv string `yaml:"token_env"`
}

// Task mirrors onair.TaskConfig: tracker ids in commit subjects -> URLs.
type Task struct {
	Pattern string `yaml:"pattern"`
	URL     string `yaml:"url"`
}

// Environment is one deploy target: a named ordered list of components.
type Environment struct {
	Components []Component `yaml:"components"`
}

// Component configures one deployable unit and its live source.
type Component struct {
	Name string      `yaml:"name"`
	Live *LiveSource `yaml:"live"`
}

// LiveSource picks exactly one live provider; absent means Assumed = Green.
type LiveSource struct {
	Probe   string `yaml:"probe"`   // /-/version URL
	Command string `yaml:"command"` // stdout is the answer (see DESIGN.md)
}

// Load reads and validates one config file.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := f.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &f, nil
}

// Find walks up from dir looking for DefaultFileName.
func Find(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		path := filepath.Join(dir, DefaultFileName)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found in %s or any parent", DefaultFileName, dir)
		}
		dir = parent
	}
}

func (f *File) validate() error {
	if f.Forge.Repo == "" {
		return fmt.Errorf("forge.repo is required")
	}
	if f.Forge.Kind == "" {
		f.Forge.Kind = "gitlab"
	}
	if f.Forge.Kind != "gitlab" {
		return fmt.Errorf("forge.kind %q is not supported yet (only gitlab)", f.Forge.Kind)
	}
	if f.Forge.Branch == "" {
		f.Forge.Branch = "main"
	}
	if f.Forge.TokenEnv == "" {
		f.Forge.TokenEnv = "GITLAB_TOKEN"
	}
	if f.Project == "" {
		f.Project = filepath.Base(f.Forge.Repo)
	}
	switch f.Attribution {
	case "":
		f.Attribution = "auto"
	case "off", "on", "auto":
	default:
		return fmt.Errorf("attribution must be off, on or auto; got %q", f.Attribution)
	}
	if len(f.Environments) == 0 {
		return fmt.Errorf("at least one environment is required")
	}
	for name, env := range f.Environments {
		if len(env.Components) == 0 {
			return fmt.Errorf("environment %q has no components", name)
		}
		for _, c := range env.Components {
			if c.Name == "" {
				return fmt.Errorf("environment %q: every component needs a name", name)
			}
			if c.Live != nil && c.Live.Probe != "" && c.Live.Command != "" {
				return fmt.Errorf("component %q: live takes probe or command, not both", c.Name)
			}
		}
	}
	return nil
}
