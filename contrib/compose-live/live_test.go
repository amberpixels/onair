//go:build !windows

package composelive_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amberpixels/onair/live"
)

// stubDocker answers `docker ps` with the ps file and `docker inspect <id>`
// with the inspect_<id> file, and records every call's arguments.
const stubDocker = `#!/bin/sh
dir=$(dirname "$0")
echo "$*" >> "$dir/calls"
case $1 in
ps) cat "$dir/ps" ;;
inspect) for last; do :; done; cat "$dir/inspect_$last" ;;
esac
`

type container struct {
	id, psState string
	inspect     string // status|health|revision, as the script's inspect format prints it
}

// run executes live.sh with a stub docker that knows the given containers,
// newest first, and returns stdout, stderr, the exit code and the stub's calls.
func run(t *testing.T, cs []container, args ...string) (stdout, stderr string, code int, calls string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("docker", stubDocker, 0o755)
	var ps strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&ps, "%s %s\n", c.id, c.psState)
		write("inspect_"+c.id, c.inspect+"\n", 0o644)
	}
	write("ps", ps.String(), 0o644)

	script, err := filepath.Abs("live.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(script, args...)
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		code = ee.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "calls"))
	return out.String(), errOut.String(), code, string(raw)
}

const sha = "9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d"

func TestLiveSuccess(t *testing.T) {
	tests := []struct {
		name       string
		containers []container
		state      string
	}{
		{"running without healthcheck", []container{{"c1", "running", "running||" + sha}}, "running"},
		{"running and healthy", []container{{"c1", "running", "running|healthy|" + sha}}, "healthy"},
		{"running and unhealthy", []container{{"c1", "running", "running|unhealthy|" + sha}}, "unhealthy"},
		{"restarting wins over health", []container{{"c1", "restarting", "restarting|healthy|" + sha}}, "restarting"},
		{"exited is reported", []container{{"c1", "exited", "exited||" + sha}}, "exited"},
		{"running container beats a newer exited one", []container{
			{"new", "exited", "exited||0000000"},
			{"old", "running", "running||" + sha},
		}, "running"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code, _ := run(t, tt.containers, "shop", "worker")
			if code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			want := `{"commit":"` + sha + `","state":"` + tt.state + `"}` + "\n"
			if stdout != want {
				t.Fatalf("stdout = %q, want %q", stdout, want)
			}
			info, err := live.ParseOutput(stdout)
			if err != nil {
				t.Fatal(err)
			}
			if info.SHA != sha || info.State != tt.state {
				t.Fatalf("ParseOutput = %+v", info)
			}
		})
	}
}

func TestLiveFilters(t *testing.T) {
	_, stderr, code, calls := run(t, []container{{"c1", "running", "running||" + sha}}, "shop", "worker")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	ps, _, _ := strings.Cut(calls, "\n")
	for _, want := range []string{
		"ps -a ",
		"label=com.docker.compose.project=shop",
		"label=com.docker.compose.service=worker",
		"label=com.docker.compose.oneoff=False",
	} {
		if !strings.Contains(ps, want) {
			t.Errorf("docker ps call %q lacks %q", ps, want)
		}
	}
}

func TestLiveFailures(t *testing.T) {
	tests := []struct {
		name       string
		containers []container
		args       []string
		code       int
	}{
		{"bad usage", nil, []string{"shop"}, 2},
		{"no container", nil, []string{"shop", "worker"}, 3},
		{"no label", []container{{"c1", "running", "running||"}}, []string{"shop", "worker"}, 4},
		{"label missing from map", []container{{"c1", "running", "running||<no value>"}}, []string{"shop", "worker"}, 4},
		{"label with a quote", []container{{"c1", "running", `running||abc"def`}}, []string{"shop", "worker"}, 4},
		{"label with a separator", []container{{"c1", "running", "running||abc|def"}}, []string{"shop", "worker"}, 4},
		{"unexpected state", []container{{"c1", "running", "run ning||" + sha}}, []string{"shop", "worker"}, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code, _ := run(t, tt.containers, tt.args...)
			if code != tt.code {
				t.Fatalf("exit %d, want %d (stderr %q)", code, tt.code, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want none", stdout)
			}
			if stderr == "" {
				t.Fatal("no message on stderr")
			}
		})
	}
}
