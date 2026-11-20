# compose-live

`live.sh` tells onair which commit a Docker Compose service is running on a
host, and whether its container is serving. It reads the image label
`org.opencontainers.image.revision`, so the app needs no code and no HTTP
endpoint, which makes it work for workers and bots too.

```
$ live.sh shop worker
{"commit":"9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d","state":"running"}
```

The output follows onair's [`command` contract](../../docs/command-contract.md).

## 1. Stamp the image

Add two lines to the final stage of the Dockerfile. Put them late, because a
label that changes on every commit invalidates the cache of every layer after
it.

```dockerfile
ARG VERSION
LABEL org.opencontainers.image.revision=$VERSION
```

Pass the commit at build time:

```sh
docker build --build-arg VERSION="$(git rev-parse HEAD)" .
```

In GitLab CI that is `--build-arg VERSION=$CI_COMMIT_SHA`. With Compose
building the image, set it under `build.args` in `compose.yml`.

## 2. Install the reader on the host

Install it at a pinned onair tag, so the box does not change behavior when
`main` moves:

```sh
curl -fsSL https://raw.githubusercontent.com/amberpixels/onair/<tag>/contrib/compose-live/live.sh \
  -o /usr/local/bin/onair-live
chmod +x /usr/local/bin/onair-live
```

The SSH user onair connects as needs access to Docker (the `docker` group, or
a sudo rule for this one script).

## 3. Point onair at it

```yaml
- name: worker
  live: { command: "ssh prod onair-live shop worker" }
```

The first argument is the Compose project name, which `docker compose ls`
shows (by default the name of the directory holding `compose.yml`). The second
is the service name from `compose.yml`. `example.onair.yml` is a full config.

## What it reports

- **commit**: the revision label of the service's container. With several
  containers (a scaled service), the first running one is read, else the
  newest. One-off `docker compose run` containers are ignored.
- **state**: `State.Status` (`running`, `restarting`, `exited`...), or the
  healthcheck status (`healthy`, `unhealthy`, `starting`) when the container
  runs and has one. onair counts `running` and `healthy` as serving and marks
  any other state, so a restart loop on a new image does not read as live.

Exit codes: 2 for bad usage, 3 when no container matches, 4 when there is no
usable revision label or state (the label is missing or not a plain revision,
or docker reports a state that is not a plain token). On any non-zero exit
onair falls back to
`Live (assumed = green)`.

Later versions of the script only add fields to its output, never change or
remove one, because installed copies stay at their tag.

The label says which image a container runs, not that the app inside answers.
A healthcheck closes most of that gap. The `/-/version` probe closes all of
it, where the app can serve one.
