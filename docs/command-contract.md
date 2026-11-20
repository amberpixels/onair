# The `command` live source contract

A `command` source runs a shell command (`sh -c`) and reads its stdout as the
answer to "which commit is running". Any tool that can answer that becomes a
live source this way, so onair has no per-platform adapters:

```yaml
components:
  - name: backend
    live:
      command: kubectl get deploy api -o jsonpath='{.spec.template.spec.containers[0].image}' # k8s
      # command: flyctl image show --json | jq -r .Tag                                        # fly.io
      # command: ssh prod onair-live shop app                                                 # compose host (contrib/compose-live)
      # command: heroku releases:info --json -a myapp | jq -r .description                    # heroku
```

## Output

stdout takes one of two forms.

**A single token**, the first word of the first line:

- a commit SHA, full or short
- a tag, or anything else the forge can resolve to a commit
- an image ref such as `registry.example.com:5000/shop/web:a1b2c3d`, where the tag
  after the last `:` is the commit
- a tag in the `sha-a1b2c3d` CI convention, where the `sha-` prefix is dropped

**A JSON object**, for richer data:

```json
{"commit": "9e8d7c6", "builtAt": "2026-07-13T09:00:00Z", "state": "restarting"}
```

- `commit` is required, in any of the token forms above.
- `builtAt` is optional, in RFC 3339. A value that does not parse is ignored.
- `state` is optional: the container state, shown as given. `running` and
  `healthy` mean serving. Any other value marks the component as not serving,
  so a restart loop on a new image does not read as live. Examples are
  `restarting`, `unhealthy`, `starting` and `exited`. Without `state`, the component
  counts as serving.

## Failure

A non-zero exit, empty output or JSON with no `commit` means Live is
unavailable. The component falls back to Green, labeled `assumed`, and the
error is printed as a note under the report.

## Confidence

A command's answer is `reported`: the user's tooling asserted it, onair did not
observe it. It ranks below `probed` (the app answering `/-/version` itself) and
above `recorded` (the forge's deployment record) and `assumed`.

## The pipe form

The same contract backs `-live-ref -`, which reads the output from stdin
instead of running a command:

```sh
ssh prod onair-live shop worker | onair -live-ref - -component worker
```

## Compatibility

Fields are only ever added to this contract, never changed or removed, because
scripts like [`contrib/compose-live/live.sh`](../contrib/compose-live) are
installed on hosts at a pinned onair tag.
