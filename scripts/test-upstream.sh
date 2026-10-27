#!/bin/sh
# Run unmodified upstream tests in private PID/network/mount namespaces.
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
mode=${1:-all}
case "$mode" in unit | regression | config | full | sdk | all) ;; *)
	echo 'Usage: sh scripts/test-upstream.sh [unit|regression|config|full|sdk|all]' >&2
	exit 2
	;;
esac
work=${LTTNG_UPSTREAM_WORK:-${TMPDIR:-$HOME/tmp}/opencode/lttng-go-upstream-2.14.0}
if [ "$(id -u)" -eq 0 ]; then
	echo 'Run this harness as an ordinary user; upstream kernel/destructive tests must not run as root.' >&2
	exit 2
fi
mkdir -p "$work"
exec 9>"$work/.lock"
flock -n 9 || {
	echo "Another upstream test run owns $work" >&2
	exit 1
}
if [ ! -d "$work/source" ]; then
	GIT_TERMINAL_PROMPT=0 git clone --depth=1 --branch=v2.14.0 \
		https://github.com/lttng/lttng-tools.git "$work/source"
fi
image=${LTTNG_UPSTREAM_IMAGE:-lttng-go-upstream:2.14.0-f44}
docker build --build-arg "TEST_UID=$(id -u)" --build-arg "TEST_GID=$(id -g)" \
	-t "$image" "$repo/scripts/upstream"
docker run --rm --init --cap-drop ALL --security-opt no-new-privileges \
	--shm-size=1g --ulimit nofile=65536:65536 \
	--mount "type=bind,src=$repo,dst=/sdk,readonly" \
	--mount "type=bind,src=$work,dst=/work" \
	--mount "type=bind,src=$(go env GOROOT),dst=/opt/go,readonly" \
	"$image" bash /sdk/scripts/upstream/run.sh "$mode"
