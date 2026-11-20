# onair-go - design notes, decisions, open questions

Companion to `README.md` (the README-driven front page). This file is the
scratchpad: why the design is shaped this way, how it maps to the Ruby gem, what
is still undecided, and the plan to validate it with a first consumer app.

Status: **v1 implemented** (core + gitlab forge +
probe/command/environment/assumed + tty/json render + CLI, all tested). Open questions below are marked resolved
where the code made the call; the rest still need a decision.

---

## How this maps to Ruby onair (`../onair-cli`)

Reading the real gem's `lib/onair.rb` and README, its seams are already the right
ones - we are generalizing, not redesigning.

| Ruby onair                         | onair-go equivalent                          |
|------------------------------------|----------------------------------------------|
| `Platform` adapter (Heroku)        | **LiveProvider** (command / probe / environment / assumed) |
| `RemoteHead` (GitHub API + ls-remote) | **Forge.Head**                            |
| local `Git` commit lookup          | **Forge.Resolve** (or a local git impl)      |
| `Snapshot{deployed, pending, ...}` | per-component **Live** ref + confidence + rollout |
| `Report` rules engine              | core engine, **`onair.Collect`** (root package) |
| `Renderer::TTY` / `::JSON`         | `render` package (tty + json)                 |
| `.onair.yml`, task links           | same config file, extended for components     |

### The one real divergence: no slug

Heroku hands Ruby onair the deployed commit directly (from the running slug), so
its `Snapshot` *is* ground truth and it never needs a "green" tier. We have no
slug, so we split that single "deployed" into two: **Green** (free, from CI, but a
proxy) and **Live** (true, but needs a probe). Everything else follows from that
split - the confidence flag, the "assumed = green" labeling, the stall detection.

### Concepts we inherit but reshape

- Ruby `Pending` (in-flight deploy) -> a pending **Rollout** on the component's
  Live: a deploy newer than Live that has not succeeded yet. GitLab deployment
  records report it today; a failed deploy is a failed Rollout.
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
2. **Confidence is a first-class field**, not a boolean, ranked strongest
   first: `Probed > Reported > Recorded > Assumed`. Probed is the app itself
   answering, Reported a configured command asserting the commit, Recorded the
   forge's deployment record, Assumed Green standing in. Drives honest
   rendering.
3. **Components are a named ordered list**, each with its own providers. Handles
   multi-backend, separate web image, mono vs multi repo, and drift - all one model.
4. **Rendering is not the library's job.** Core returns a `Report`; CLI and admin
   panel are two renderers. Keeps it usable in a terminal and a Vue popover from
   day one.
5. **GitLab-first, Forge-abstract.** We only write the GitLab impl now, but behind
   the interface, so GitHub is a later drop-in, not a rewrite.
6. **Real Live comes from conventions, not requirements.** For container
   deploys the default is the standard image label
   `org.opencontainers.image.revision`: two Dockerfile lines, no app code, and
   it covers workers and bots with no HTTP surface. `/-/version` is the opt-in,
   higher-fidelity probe we ship helpers for, because it proves the app
   answers, not only that its container runs. Absence of both degrades to the
   Green tier.
7. **`command` is the universal Live provider - no per-platform adapters, ever.**
   Instead of onair knowing about k8s / AWS / Fly / bare VMs / Heroku, the config
   supplies an arbitrary command whose output *is* the answer to "what is live."
   Same pattern as git credential helpers, kubectl exec plugins, Terraform's
   `external` data source. Litmus test: Ruby onair's entire `Platform::Heroku`
   adapter collapses into a one-line `heroku releases:info --json | jq ...`
   command - if the abstraction absorbs the original tool's only adapter, it is
   general enough. This does **not** replace the probe: `command` assumes a
   machine with the CLI and credentials (kubectl context, SSH keys), which the
   embedded/library case (an admin route running inside the backend) does not
   have. Provider lineup: **command** (universal CLI adapter), **probe**
   (embedded / `/-/version` convention), **environment** (the forge's
   deployment records), **assumed** (fallback).
   Reading the forge's own deployment records (`environment`, `Recorded`
   confidence) does not break this rule: it is a forge capability, like
   pipelines, not a per-platform adapter. It serves viewers with project read
   access but no host access.

### The `command` provider contract

```yaml
components:
  - name: backend
    live:
      command: kubectl get deploy api -o jsonpath='{.spec.template.spec.containers[0].image}' # k8s
      # command: flyctl image show --json | jq -r .Tag                                        # fly.io
      # command: ssh prod onair-live shop app                                                 # compose host (contrib/compose-live)
      # command: heroku releases:info --json -a myapp | jq -r .description                    # heroku
```

- **stdout**: a commit SHA, or anything resolvable to one (tag, image ref with a
  SHA suffix) - resolution goes through `Forge.Resolve`, which already exists.
  Optionally a small JSON `{commit, builtAt, state}` for richer data. `state`
  is the container state: `running` and `healthy` mean serving; any other
  value (`restarting`, `unhealthy`, `starting`, `exited`...) is shown as given
  and marks the component as not serving, so a restart loop on a new image is
  not reported as live. Without `state`, nothing changes.
- **non-zero exit**: Live unavailable -> degrade to `Assumed = Green`, the
  existing fallback path. Nothing new.
- **confidence**: `Reported` - user-asserted truth onair did not observe
  itself, ranked below `Probed` (see open question 4).
- **two invocation shapes**, same contract: onair execs the configured
  `live.command` itself, and an ad-hoc pipe/flag form for one-offs:
  `kubectl ... | onair --live-ref -`.

`contrib/compose-live/live.sh` is the reference command for Docker Compose
hosts: it reads the revision label and the container's state and prints
`{commit, state}`. Hosts install it at a pinned onair tag, so its output follows
this contract's rule: fields are only ever added.

---

## Open questions (need a decision)

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
   not adapters we write. **Resolved by `Reported`**: command output gets its
   own confidence, ranked below `Probed`, and `ImageTag` is gone from decision
   2's enum. For an app whose deploy sets
   `IMAGE_TAG=$CI_COMMIT_SHORT_SHA`, a command reading that env var works from a
   terminal, but the embedded admin route still wants the probe. The OCI
   revision label (decision 6) replaces reading `IMAGE_TAG`, because it travels
   with the image rather than the deploy's environment.
5. ~~Attribution `auto` gate data source.~~ **Resolved as the leaning**:
   recent-history author set (via `Forge.Recent(branch, 50)`, an interface
   addition), with the alias map on top. Forge member list stays a possible
   upgrade.
6. **Multiple environments in one view?** Ruby shows one app. Keep one environment
   per invocation (`--env prod`) and let the admin panel switch, vs show all at
   once. Leaning: one at a time.

---

## A first consumer app (validation plan, not a build order)

The point of building the lib is to keep the consumer app clean while proving
the interfaces. The app side stays small:

1. **Stamp the backend** - a `buildinfo` package (`Commit`, `BuiltAt`), injected
   via `-ldflags -X` from a Docker build-arg set in CI. Expose `GET /-/version`.
2. **Stamp the frontend** - a `VITE_COMMIT_SHA` build variable, served as the
   web `/-/version`.
3. **Backend endpoint** - an admin-only route that builds onair's config
   programmatically (GitLab forge + probe live providers for backend/web),
   calls the core and returns the `Report` JSON. Needs a **read-only GitLab API
   token** in the prod environment.
4. **Admin popover** - a small button in the admin topbar that renders the
   `Report`. Admin-gated; attribution mode `auto` maps the logged-in admin's
   email to git authors.

---

## Things a senior reviewer would flag

- **Rollback/stall detection is the killer feature.** The probe delivers it, and
  so do the OCI label reader (`contrib/compose-live`, with container state) and
  the forge's deployment records (with pending and failed rollouts). Without a
  live source we ship a fancy version string, so these must stay first-class.
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
