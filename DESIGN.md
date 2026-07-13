# onair-go - design notes, decisions, open questions

Companion to `README.md` (the README-driven front page). This file is the
scratchpad: why the design is shaped this way, how it maps to the Ruby gem, what
is still undecided, and the plan to validate it with p44.

Status: **v1 implemented** (core + gitlab forge + probe/command/assumed +
tty/json render + CLI, all tested). Open questions below are marked resolved
where the code made the call; the rest still need a decision.

---

## How this maps to Ruby onair (`../onair-cli`)

Reading the real gem's `lib/onair.rb` and README, its seams are already the right
ones - we are generalizing, not redesigning.

| Ruby onair                         | onair-go equivalent                          |
|------------------------------------|----------------------------------------------|
| `Platform` adapter (Heroku)        | **LiveProvider** (command / probe / assumed) |
| `RemoteHead` (GitHub API + ls-remote) | **Forge.Head**                            |
| local `Git` commit lookup          | **Forge.Resolve** (or a local git impl)      |
| `Snapshot{deployed, pending, ...}` | per-component **Live** ref + confidence       |
| `Report` rules engine              | core **report** package                       |
| `Renderer::TTY` / `::JSON`         | `render` package (tty + json)                 |
| `.onair.yml`, task links           | same config file, extended for components     |

### The one real divergence: no slug

Heroku hands Ruby onair the deployed commit directly (from the running slug), so
its `Snapshot` *is* ground truth and it never needs a "green" tier. We have no
slug, so we split that single "deployed" into two: **Green** (free, from CI, but a
proxy) and **Live** (true, but needs a probe). Everything else follows from that
split - the confidence flag, the "assumed = green" labeling, the stall detection.

### Concepts we inherit but reshape

- Ruby `Pending` (in-flight build) -> our **Green tier + pipeline status**. A
  running pipeline on a commit newer than Green is our "pending."
- Ruby `pinned` (newer build succeeded but not running / rollback) -> falls out
  of comparing **Live vs Green**: Live behind Green = pinned/stalled/rolled back.
- Ruby `yours` (your commit absorbed by a later deploy) -> our **attribution**,
  but generalized to any tier and gated by the 3 modes.
- Ruby `task:` links (Jira/Linear id in subject -> URL) -> **keep as-is**, it is
  orthogonal and genuinely nice. Port the `task.pattern` / `task.url` config.

---

## Key decisions (proposed, open to change)

1. **Three explicit truth tiers (HEAD / Green / Live)** rather than onair's binary
   git-vs-deployed. Rationale: our deploy path can't self-report by default, so we
   must be explicit about how much we actually know.
2. **Confidence is a first-class field**, not a boolean. `Probed / ImageTag /
   Assumed`. Drives honest rendering.
3. **Components are a named ordered list**, each with its own providers. Handles
   multi-backend, separate web image, mono vs multi repo, and drift - all one model.
4. **Rendering is not the library's job.** Core returns a `Report`; CLI and admin
   panel are two renderers. Keeps it usable in a terminal and a Vue popover from
   day one.
5. **GitLab-first, Forge-abstract.** We only write the GitLab impl now, but behind
   the interface, so GitHub is a later drop-in, not a rewrite.
6. **`/-/version` is a convention we ship helpers for**, not a hard requirement.
   Adoption is copy-paste; absence degrades to the Green tier.
7. **`command` is the universal Live provider - no per-platform adapters, ever.**
   Instead of onair knowing about k8s / AWS / Fly / Hetzner / Heroku, the config
   supplies an arbitrary command whose output *is* the answer to "what is live."
   Same pattern as git credential helpers, kubectl exec plugins, Terraform's
   `external` data source. Litmus test: Ruby onair's entire `Platform::Heroku`
   adapter collapses into a one-line `heroku releases:info --json | jq ...`
   command - if the abstraction absorbs the original tool's only adapter, it is
   general enough. This does **not** replace the probe: `command` assumes a
   machine with the CLI and credentials (kubectl context, SSH keys), which the
   embedded/library case (p44 admin route running inside the backend) does not
   have. Provider lineup: **command** (universal CLI adapter), **probe**
   (embedded / `/-/version` convention), **assumed** (fallback).

### The `command` provider contract

```yaml
components:
  backend:
    live:
      command: kubectl get deploy api -o jsonpath='{.spec.template.spec.containers[0].image}' # k8s
      # command: flyctl image show --json | jq -r .Tag                                        # fly.io
      # command: ssh prod docker inspect app --format '{{index .Config.Labels "commit"}}'     # hetzner box
      # command: heroku releases:info --json -a myapp | jq -r .description                    # heroku
```

- **stdout**: a commit SHA, or anything resolvable to one (tag, image ref with a
  SHA suffix) - resolution goes through `Forge.Resolve`, which already exists.
  Optionally a small JSON `{commit, builtAt, version}` for richer data.
- **non-zero exit**: Live unavailable -> degrade to `Assumed = Green`, the
  existing fallback path. Nothing new.
- **confidence**: user-asserted ground truth, so `Probed`-grade - or a distinct
  `Reported` label (see open question 4).
- **two invocation shapes**, same contract: onair execs the configured
  `live.command` itself, and an ad-hoc pipe/flag form for one-offs:
  `kubectl ... | onair --live-ref -`.

---

## Open questions (need Eugene's call)

1. ~~Naming.~~ **Resolved by the repo itself**: module `github.com/amberpixels/onair`
   (this repo - the name was reserved for exactly this), binary `onair`. The $PATH
   collision with the Ruby gem's binary is accepted; it is rare to install both.
2. ~~Does the Go core also render the CLI?~~ **Resolved as the leaning**: core
   returns `Report`, package `render` (tty via charmbracelet/lipgloss + json) is a
   separate consumer, `cmd/onair` is thin wiring.
3. ~~Live provider default when nothing is configured.~~ **Resolved as the
   leaning**: assumed+labeled - the engine substitutes Green with
   `LiveConfidence = Assumed` when a component has no provider (or its provider
   fails), because that is still the most useful single line.
4. ~~How does `image` live-provider read the running tag?~~ **Dissolved into
   `command`** (decision 7): SSH + `docker inspect`, Docker API socket, reading
   the compose `IMAGE_TAG` - all are just whatever command the user configures,
   not adapters we write. What remains of this question: does command output get
   `Probed` confidence or its own `Reported` label (which would replace
   `ImageTag` in decision 2's enum)? For p44, where the deploy sets
   `IMAGE_TAG=$CI_COMMIT_SHORT_SHA`, a command reading that env var works from a
   terminal, but the embedded admin route still wants the probe.
5. ~~Attribution `auto` gate data source.~~ **Resolved as the leaning**:
   recent-history author set (via `Forge.Recent(branch, 50)`, an interface
   addition), with the alias map on top. Forge member list stays a possible
   upgrade.
6. **Multiple environments in one view?** Ruby shows one app. Keep one environment
   per invocation (`--env prod`) and let the admin panel switch, vs show all at
   once. Leaning: one at a time.

---

## p44 as consumer #1 (validation plan, not a build order)

The point of building the lib is to keep p44 clean while proving the interfaces.
When we do build, the p44 side is small:

1. **Stamp the backend** - add `internal/buildinfo` (`Commit`, `BuiltAt`), inject
   via `-ldflags -X` from a Docker build-arg, plumbed through `.gitlab-ci.yml`
   exactly like `VITE_COMMIT_SHA` already is (frontend already stamps; backend does
   not). Expose `GET /-/version`.
   - Files today: `Dockerfile` (plain `go build`, no ldflags), `justfile` build
     recipe, `.gitlab-ci.yml` (web gets `--build-arg VITE_COMMIT_SHA`, backend gets
     nothing). The `/-/` namespace lives in
     `internal/application/api/router.go` (pattern: `root.HandleFunc("GET /-/...")`).
2. **Frontend already has** `VITE_COMMIT_SHA` + `CommitBadge.vue` (superadmin-only).
   The web `/-/version` is basically exposing what the badge already reads.
3. **Backend endpoint** - a superadmin admin route that constructs onair-go's
   config programmatically (GitLab forge + probe live providers for backend/web),
   calls the core, returns the `Report` JSON. Needs a **read-only GitLab API token**
   in prod env.
4. **Admin popover** - small icon-button in `AdminLayout.vue`'s topbar action group
   (next to "View Map"), `el-popover trigger="click"`, renders the `Report`.
   Superadmin-gated (matches the CommitBadge visibility). Attribution mode: `auto`
   maps the logged-in admin email to git authors.

None of this happens until the lib's interfaces feel right on paper.

---

## Things a senior reviewer would flag

- **Rollback/stall detection is the killer feature and only the probe delivers it.**
  If we ship without probes, we ship a fancy version string. The probe convention
  must land early, or the tool doesn't earn its thesis.
- **Email identity is a swamp.** Noreply addresses, squash-merge author rewriting,
  bot commits. Alias map is table stakes; don't over-promise `auto` accuracy.
- **Forge rate limits / latency.** Cache forge calls ~30-60s; race API vs a cheap
  fallback like Ruby does (GitHub API vs `git ls-remote`).
- **Security/privacy.** The report leaks author emails + CI internals; gate it
  (superadmin) and let `auto` double as a privacy filter for non-devs.
- **Time skew and "N behind" semantics.** Be precise: Live is N behind Green is M
  behind HEAD; three numbers, don't collapse them.
- **Clock/`builtAt` trust.** `builtAt` from the artifact vs commit time from the
  forge can disagree; prefer commit time for "age," keep `builtAt` for "is this a
  stale image."
