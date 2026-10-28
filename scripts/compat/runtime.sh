#!/bin/bash
set -euo pipefail
series=$1
work=$(mktemp -d "$TMPDIR/runtime-$series.XXXXXX")
export LTTNG_HOME="$work" LTTNG_RUNDIR="$work/.lttng"
export LTTNG_UST_CTL_PATH="$LTTNG_RUNDIR" LTTNG_UST_APP_PATH="$LTTNG_RUNDIR"
export LTTNG_CTL_GO_INTEGRATION=1
mkdir -p "$LTTNG_RUNDIR" "$work/empty-sessions"
daemon_pid=
cleanup() {
	status=$?
	trap - EXIT
	if [[ -n $daemon_pid ]]; then
		kill -TERM "$daemon_pid" 2>/dev/null || true
		wait "$daemon_pid" 2>/dev/null || true
	fi
	if [[ $status != 0 ]]; then cat "$work/sessiond.log" >&2; else rm -r "$work"; fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
lttng-sessiond --no-kernel --load="$work/empty-sessions" >"$work/sessiond.log" 2>&1 &
daemon_pid=$!
for ((attempt = 0; ; attempt++)); do
	[[ ! -S $LTTNG_RUNDIR/client-lttng-sessiond ]] || break
	if ! kill -0 "$daemon_pid" 2>/dev/null || [[ $attempt -ge 100 ]]; then
		echo 'sessiond failed to start' >&2
		exit 1
	fi
	sleep 0.05
done
/work/bin/root.test -test.v -test.timeout=180s
if [[ $series == 2.14 ]]; then
	(cd /sdk/ctl && /work/bin/ctl.test -test.v -test.timeout=180s)
else
	(cd /sdk/ctl && /work/bin/ctl.test -test.v -test.timeout=180s \
		'-test.run=TestEveryAPI|TestUnavailableAPI|TestCompatibility')
fi
