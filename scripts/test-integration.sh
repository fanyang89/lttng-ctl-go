#!/bin/sh
# Start an isolated session daemon; never use or stop the host's existing daemon.
set -eu

cd "$(dirname "$0")/.."
base="${TMPDIR:-$HOME/tmp}/opencode"
mkdir -p "$base"
work=$(mktemp -d "$base/lttng-go.XXXXXX")
export LTTNG_HOME="$work"
export LTTNG_RUNDIR="$work/.lttng"
export LTTNG_UST_CTL_PATH="$LTTNG_RUNDIR"
export LTTNG_UST_APP_PATH="$LTTNG_RUNDIR"
export LTTNG_GO_INTEGRATION=1
mkdir -p "$LTTNG_RUNDIR" "$work/empty-sessions"
daemon_pid=

cleanup() {
	status=$?
	trap - EXIT
	if [ -n "$daemon_pid" ]; then
		kill -TERM "$daemon_pid" 2>/dev/null || true
		wait "$daemon_pid" 2>/dev/null || true
	fi
	if [ "$status" -ne 0 ]; then
		cat "$work/sessiond.log" >&2
		printf 'Integration artifacts: %s\n' "$work" >&2
	else
		rm -r -- "$work"
	fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

lttng-sessiond --no-kernel --load="$work/empty-sessions" \
	--pidfile="$work/sessiond.pid" >"$work/sessiond.log" 2>&1 &
daemon_pid=$!

attempt=0
while [ ! -S "$LTTNG_RUNDIR/client-lttng-sessiond" ]; do
	if ! kill -0 "$daemon_pid" 2>/dev/null || [ "$attempt" -ge 100 ]; then
		printf 'Isolated session daemon failed to start.\n' >&2
		exit 1
	fi
	attempt=$((attempt + 1))
	sleep 0.05
done
go test -tags=integration -count=1 "$@" ./...
