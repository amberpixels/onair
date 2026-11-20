<p align="center">
  <img src="logo.svg" alt="onair" width="380">
</p>

<div align="center">

### Green is not Live.

See which commit each service of a deploy is actually running.

[![Go Version](https://img.shields.io/github/go-mod/go-version/amberpixels/onair)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)](LICENSE)

</div>

---

onair compares three tiers for one environment. **HEAD** is the latest commit
on the branch. **Green** is the latest commit whose pipeline passed. **Live** is
what each running component says it runs. A stalled deploy, a rollback or a
restart loop shows up as the gap between them.

```
  shop · prod                                  gitlab.com/acme/shop

  HEAD    a1b2c3d (2h ago) by Alice
  → Fix the checkout total • ↗ !1234

  Green   a1b2c3d (2h ago) by Alice  ★ == HEAD
  → Fix the checkout total • ↗ !1234

  Live    9e8d7c6 (3h ago) by Bob  ↓ 1 behind green · 1 behind main · ✓ yours
    backend 9e8d7c6 · worker 9e8d7c6 ✗ restarting (reported)   ✗ worker not serving
  → Add the widget API • ↗ !1230
```

When every tier agrees, the report collapses to one line:

```
  Deployed: a1b2c3d (2h ago) by Alice  ★ current
  → Fix the checkout total • ↗ !1234
```

It is the Go sibling of [onair-cli](https://github.com/amberpixels/onair-cli),
which does the same for a single Heroku app by reading its slug. Here a deploy
is a list of components, such as backend, web and worker, and the forge is
GitLab. There is no slug, so each component needs a live source.

## Install

```sh
go install github.com/amberpixels/onair/cmd/onair@latest
```

## Quick Start

```sh
# .onair.yml is found upward from the working directory, like git's root
cat > .onair.yml <<'EOF'
forge: { repo: acme/shop }
environments:
  prod:
    components:
      - name: backend
        live: { probe: "https://shop.example/api/-/version" }
      - name: worker
        live: { command: "ssh prod onair-live shop worker" }
EOF

export GITLAB_TOKEN=...      # read_api; not needed for a public project
onair                        # the only environment, rendered for the terminal
onair -env prod -json        # the same Report as JSON
```

## Live Sources

Each component takes one `live:` source. The report labels how much it knows,
from strongest to weakest:

| Source | Config | Confidence |
|---|---|---|
| A `/-/version` endpoint returning `{"commit","builtAt"}` | `probe: <url>` | probed |
| A shell command printing a SHA, a tag, an image ref, or `{"commit","builtAt","state"}` | `command: <cmd>` | reported |
| The newest successful GitLab deployment of an environment | `environment: <name>` | recorded |
| None: Green stands in | `live: {}`, or no source at all | assumed |

- **command** is the universal adapter: `ssh` + `docker inspect`, `kubectl`,
  `flyctl`. A `state` other than `running` or `healthy` marks the component as
  not serving.
- **environment** also shows a newer deploy still running (`⟳ deploying`) or
  failed (`✗ deploy of <sha> failed`). It needs only read access to the
  project, so it works for people who cannot reach the hosts. Set it once on
  the environment to apply it to every component without a source of its own.
- **Docker Compose hosts**: stamp the commit into the image with the OCI label
  `org.opencontainers.image.revision`, and
  [`contrib/compose-live`](contrib/compose-live) reads it on the host as a
  `command` source. No app code, so it works for workers too.

## Usage

```
onair [flags]
```

| Flag | Meaning |
|---|---|
| `-env <name>` | environment to show; required when more than one is configured |
| `-json` | print the Report as JSON instead of the terminal view |
| `-branch <name>` | override the tracked branch |
| `-live-ref <ref>` | use this commit-ish as Live; `-` reads it from stdin |
| `-component <name>` | the component `-live-ref` applies to (default: the only one) |
| `-config <path>` | path to `.onair.yml` |
| `-timeout <d>` | overall deadline (default `15s`) |
| `-no-color` | disable colors |

`-live-ref -` takes the same output as a `command` source, so a CI job can pipe
it in: `ssh prod onair-live shop worker | onair -live-ref - -component worker`.

## Configuration

```yaml
project: shop                # default: the repo's last path segment
forge:
  kind: gitlab               # the only forge so far
  repo: acme/shop
  branch: main               # default main
  base_url: https://gitlab.example   # default gitlab.com
  token_env: GITLAB_TOKEN    # the variable holding the token (default)
attribution: auto            # off | on | auto: mark commits as "✓ yours"
identities:
  bob: [bob@example.com, bob@work.example]
task:                        # link tracker ids found in commit subjects
  pattern: 'PROJ-\d+'
  url: https://tracker.example/{id}
environments:
  prod:
    components:
      - name: backend
        live: { probe: "https://shop.example/api/-/version" }
      - name: worker
        live: { command: "ssh prod onair-live shop worker" }
  staging:
    environment: staging     # GitLab deployments are Live for every component below
    components:
      - name: backend
      - name: web
        live: {}             # opt out: Assumed = Green
```

`attribution: auto` marks your commits only when your `git config user.email`,
or one of its aliases under `identities`, authored recent commits on the branch.

## As a Library

The CLI only turns the config and flags into providers and renders the result.
A host app, such as an admin panel or a status page, imports the core instead.
It wires its own providers and renders the `Report` itself:

```go
report, err := onair.Collect(ctx, onair.Params{
    Forge:      gitlab.NewClient("", "acme/shop", token),
    Branch:     "main",
    Components: []onair.Component{
        {Name: "backend", Live: &live.Probe{URL: "https://shop.example/api/-/version"}},
    },
    Identity:    viewer,                // who is looking, for "yours"
    Attribution: onair.AttributionAuto,
})
```

The JSON shape of `Report` is a public API and only changes additively.

## How It Works

onair is read-only: it calls the GitLab API, runs your `command` sources and
`git config user.email`, and never deploys or writes anything. A failing source
never stops the report. That component falls back to Green, labeled as assumed,
and the error is printed as a note.

A few decisions shape the rest:

- **Green is not Live.** They match only when every deploy ships the latest
  green commit and nothing stalls or rolls back. So Live is its own tier, and
  Green stands in for it only with an `assumed` label.
- **Confidence is a field, not a flag.** Every Live answer says how it is known,
  so the report never claims more than its weakest source.
- **No per-platform adapters.** `command` turns any CLI into a live source
  ([contract](docs/command-contract.md)). Deployment records are a forge
  feature, like pipelines, so they are the one built-in source besides the probe.
- **Rendering is the host's job.** The core returns a `Report`, and the
  terminal view and the JSON are two renderers of it.
- **GitLab first, not GitLab only.** The forge is an interface, so GitHub can
  be added without touching the core.

## License

[MIT](LICENSE) © [amberpixels](https://amberpixels.io)
