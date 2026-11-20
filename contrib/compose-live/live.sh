#!/bin/sh
# live.sh - report what a Docker Compose service is running, for onair's
# `command` live provider.
#
# Usage: live.sh <compose project> <service>
#
# Prints one line of JSON, {"commit":"<sha>","state":"<state>"}, where the
# commit is the container's org.opencontainers.image.revision label and the
# state is State.Status, or the healthcheck status when the container runs
# and has one. Copies are installed on hosts at a pinned onair tag, so this
# output only ever gains fields.
#
# Exit codes: 2 bad usage, 3 no container for the service, 4 no usable
# revision label. Any non-zero exit makes onair fall back to Assumed = Green.
set -eu

REVISION_LABEL=org.opencontainers.image.revision

fail() {
	code=$1
	shift
	echo "live.sh: $*" >&2
	exit "$code"
}

[ $# -eq 2 ] || fail 2 "usage: live.sh <compose project> <service>"
project=$1
service=$2

# One-off `docker compose run` containers carry the service label too, so
# they are filtered out. -a keeps exited and restarting containers visible.
containers=$(docker ps -a \
	--filter "label=com.docker.compose.project=$project" \
	--filter "label=com.docker.compose.service=$service" \
	--filter "label=com.docker.compose.oneoff=False" \
	--format '{{.ID}} {{.State}}')

# A scaled service has several containers: the first running one wins,
# else the newest, which docker ps lists first.
id=
first=
while read -r cid cstate; do
	[ -n "$cid" ] || continue
	[ -n "$first" ] || first=$cid
	if [ "$cstate" = running ]; then
		id=$cid
		break
	fi
done <<EOF
$containers
EOF
[ -n "$id" ] || id=$first
[ -n "$id" ] || fail 3 "no container for service $service in compose project $project"

# The label goes last: the state fields never hold a '|', so a label that
# does stays whole and fails the check below.
info=$(docker inspect --format \
	"{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{end}}|{{index .Config.Labels \"$REVISION_LABEL\"}}" \
	"$id")
status=${info%%|*}
rest=${info#*|}
health=${rest%%|*}
revision=${rest#*|}

# Only plain tokens are printed, so the JSON needs no escaping.
case $revision in
'' | '<no value>') fail 4 "container $id has no $REVISION_LABEL label" ;;
*[!A-Za-z0-9._-]*) fail 4 "container $id: $REVISION_LABEL is not a plain revision" ;;
esac

state=$status
if [ "$status" = running ] && [ -n "$health" ]; then
	state=$health
fi
case $state in
'' | *[!A-Za-z0-9._-]*) fail 4 "container $id: unexpected state" ;;
esac

printf '{"commit":"%s","state":"%s"}\n' "$revision" "$state"
