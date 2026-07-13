# onair-go (draft / design)

> See what's on air - which commit is actually running, for Go backends and Vite
> frontends, across one or many services.

This is a **README-driven design draft**, not code yet. It is the Go sibling of
[`amberpixels/onair-cli`](../onair-cli) (the Ruby gem). Same one-question thesis,
different world: instead of Heroku slugs, our world is **GitLab CI + Docker
Compose deploys, multiple services (backend / web / worker), and a Vite frontend
that ships as its own image.**

The Ruby onair answers the question for a single Heroku app by reading the
running *slug*. onair-go generalizes it along three axes:

1. **Truth is layered, not binary.** Not "git vs deployed" but three tiers.
2. **Deploys are multi-component.** A named list of services, not one app.
3. **"Yours" is context-dependent.** Obvious in a CLI, learnable in an admin panel.

---

## 1. The core idea: three truth levels

The Ruby onair has it easy: Heroku's slug *is* the running commit, so there is
one deployed truth. We have no slug. So we make the layering explicit - for any
service, a commit can be at one of three tiers, in increasing fidelity:

| Tier      | Meaning                                   | Source                         |
|-----------|-------------------------------------------|--------------------------------|
| **HEAD**  | Latest commit on the tracked branch       | Forge API / git                |
| **Green** | Latest commit whose CI pipeline passed    | Forge CI API                   |
| **Live**  | The commit actually running right now      | Probe / image tag / (assumed)  |

The trap onair was built to avoid: **"Green" is not "Live."** They match only
when you always ship latest-green and never lag or roll back. A stalled deploy or
a rollback makes Green run ahead of Live - and surfacing exactly that is the
whole point. So:

- **Green** is the zero-config tier. Any forge with CI gives it, no app changes.
  When we can't probe the real running commit, we show Green **labeled as an
  assumption** ("Live (assumed = green)"), never as if it were ground truth.
- **Live** is recovered by a **probe** when the app self-reports its build commit
  (a `/-/version` endpoint, or the Vite bundle's stamped SHA). When present it
  *wins*, and we can flag `green is 2 ahead of live -> deploy stalled`.

A given setup shows as many tiers as it can supply, and degrades gracefully with
honest labels (onair principle #5).

---

## 2. Components: a deploy is a named list

Your deploy is not one app. It's `backend`, `web`, maybe `worker` - each built
and shipped as its own image, each able to report its own Live commit. onair-go
models a **Component** as a named unit with its own providers, and renders them
joined:

```
Live   backend a1b2c3d · web a1b2c3d · worker a1b2c3d      in sync
```

Drift falls out for free. Two components that should share a tag but report
different SHAs is a **deploy-drift** warning (your backend shipped, your web
didn't):

```
Live   backend a1b2c3d · web 5f4e3d2                       ⚠ web 1 behind backend
```

Monorepo (components share a repo + SHA) and multi-repo (each its own repo) are
both just "a component points at a repo" - never assume shared.

---

## 3. The three provider seams (this is the library)

Core logic is pure and asks abstract questions; three interfaces answer them.
Concrete impls are swappable, so GitLab-first does not mean GitLab-only.

### Forge - git + CI truth (vendor-neutral)

The umbrella name for a git host (GitLab, GitHub, Gitea...). Answers the abstract
questions every host can answer:

```go
type Forge interface {
    Head(ctx context.Context, branch string) (Ref, error)          // HEAD tier
    LatestGreen(ctx context.Context, branch string) (Ref, error)   // Green tier
    Resolve(ctx context.Context, sha string) (Ref, error)          // metadata for a sha
    Behind(ctx context.Context, from, to string) (int, error)      // "N behind"
    CommitURL(sha string) string
    RequestURL(id string) string                                   // MR / PR link
}
```

First impl: **GitLab** (our world). Later: **GitHub** (drop-in, same interface).

### Live - what's actually running, per component

```go
type LiveProvider interface {
    Live(ctx context.Context, c Component) (Ref, LiveConfidence, error)
}
```

Impls, in fidelity order:

- **`probe`** - GET a `/-/version` endpoint returning `{commit, builtAt}`.
  Highest fidelity. Needs the app to self-stamp (see §5).
- **`image`** - read the running container's image tag when `tag == short sha`.
  Zero app changes; works for our compose deploy.
- **`assumed`** - fall back to Green, flagged `LiveConfidence = Assumed`.

`LiveConfidence` (`Probed` / `ImageTag` / `Assumed`) is what lets the renderer be
honest about how much it actually knows.

### Identity - who is viewing (for "yours")

```go
type Identity interface {
    ViewerEmails(ctx context.Context) ([]string, error)  // may be empty
}
```

- **CLI impl**: `git config user.email`.
- **Host impl** (admin panel): the logged-in user's email, handed in.

---

## 4. "Yours" / attribution - three modes

Marks commits authored by the viewer. Ruby onair always knows "you" (it's a CLI).
In an admin panel we might not - so it's a mode:

- **`off`** - never annotate.
- **`on`** - always annotate against a configured identity.
- **`auto`** - annotate **only if the viewer's email resolves to a known git
  author**. A non-dev admin with a random email simply never sees the labels;
  the noise disappears for people it means nothing to.

`auto` needs two checks: *is this commit yours?* (viewer email == commit author
email) and, as the gate, *is the viewer a git author at all?* (viewer email is in
the author set of recent history / forge members). Because emails are messy
(GitHub noreply, work vs personal), config carries an **identity alias map**:

```yaml
identities:
  eugene: [amber.pixels.io@gmail.com, eugene@work.example]
```

---

## 5. The `/-/version` convention (opt-in, unlocks real Live)

Green-as-proxy is free but blind to stalls and rollbacks. Real Live needs the
running artifact to say what it is. onair-go proposes a tiny convention and ships
helpers so adopting it is copy-paste:

- **Go backend**: a `buildinfo` package stamped via
  `-ldflags "-X .../buildinfo.Commit=$SHA -X .../buildinfo.BuiltAt=$TS"`, exposed
  at `GET /-/version -> {"commit":"a1b2c3d","builtAt":"..."}`.
- **Vite frontend**: a plugin that stamps `VITE_COMMIT_SHA` into the bundle and a
  served `/-/version` (or a `<meta>` tag) the probe can read.

Both are optional. Without them onair-go still works at the Green tier; with them
it tells the truth.

---

## 6. Output

### Terminal (CLI renderer)

```
p44 · prod                                    gitlab.com/amberpixels/p44

HEAD    a1b2c3d  (2h ago) by Alice
  → Fix the thing  ↗ !1234

Green   a1b2c3d  (2h ago) by Alice                         ★ == HEAD
  → Fix the thing  ↗ !1234

Live    9e8d7c6  (3h ago) by Eugene   ↓ 1 behind green · 1 behind main   (yours)
  backend 9e8d7c6 · web 9e8d7c6   in sync
  → Add the widget API  ↗ !1230
```

When there is no probe, the Live header reads `Live (assumed = green)` and drops
any claim it cannot back up (onair: silence over speculation).

### JSON (public API, additive-only)

Rendering lives in the host. The Go core returns a `Report`; the CLI is one
renderer, p44's admin popover is another. Shape (draft):

```json
{
  "project": "p44",
  "environment": "prod",
  "forge": { "kind": "gitlab", "repo": "amberpixels/p44", "branch": "main" },
  "head":  { "sha": "a1b2c3d", "subject": "...", "author": "Alice", "at": "..." },
  "green": { "sha": "a1b2c3d", "pipeline": "passed", "at": "..." },
  "components": [
    { "name": "backend", "live": { "sha": "9e8d7c6", "confidence": "probed",
      "builtAt": "...", "behindGreen": 1, "behindHead": 1, "mine": true } },
    { "name": "web", "live": { "sha": "9e8d7c6", "confidence": "probed" } }
  ],
  "drift": null,
  "attribution": "auto"
}
```

---

## 7. Config (`.onair.yml`)

Zero-code for the CLI; the same shape can be built programmatically by a host.

```yaml
project: p44
forge: { kind: gitlab, repo: amberpixels/p44, branch: main }
attribution: auto            # off | on | auto
identities:
  eugene: [amber.pixels.io@gmail.com]
environments:
  prod:
    components:
      - name: backend
        live: { probe: "https://pieton.md/api/-/version" }
      - name: web
        live: { probe: "https://pieton.md/-/version" }
```

---

## 8. Principles (inherited from Ruby onair)

1. **Truth over convenience.** What's running, not what was built last.
2. **Read-only.** Never deploys, rolls back, or writes (except an explicit lazy fetch).
3. **Vendor-agnostic core.** GitLab is the first Forge, not the architecture.
4. **Degrade gracefully.** Offline, no probe, no color - always render something useful, never a stack trace.
5. **JSON schema is a public API.** Changes are additive only.

---

## Shape of the repo (proposed)

```
amberpixels/onair-go
  onair/            core: domain + provider interfaces + report rules. Pure.
  onair/gitlab      Forge impl
  onair/live        probe / image / assumed impls
  onair/render      tty + json renderers
  cmd/onair         CLI binary
  contrib/buildinfo Go self-stamp helper (§5)
  contrib/vite      Vite stamp plugin (§5)
```

Host apps import `onair/` core, wire providers, get a `Report`, render it. p44 is
consumer #1 and the test-drive - same relationship r3/k1/years have with p44.
# onair
