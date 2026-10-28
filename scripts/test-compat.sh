#!/bin/sh
# Build the SDK once, then execute the same test binaries against each runtime.
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
mode=${1:-all}
case "$mode" in build | test | all) ;; *)
	echo "Usage: $0 [build|test|all]" >&2
	exit 2
	;;
esac
work=${LTTNG_COMPAT_WORK:-${TMPDIR:-$HOME/tmp}/opencode/lttng-ctl-go-compat}
image=${LTTNG_COMPAT_IMAGE:-lttng-ctl-go-compat:debian12}
mkdir -p "$work"
exec 9>"$work/.lock"
flock -n 9 || {
	echo "Compatibility harness is already running" >&2
	exit 1
}
if [ "$(id -u)" -eq 0 ]; then
	echo "Run as an ordinary user so the tests use an isolated unprivileged sessiond" >&2
	exit 2
fi
LTTNG_COMPAT_WORK="$work" sh "$repo/scripts/compat/fetch.sh"
if [ "${LTTNG_COMPAT_SKIP_IMAGE_BUILD:-0}" != 1 ]; then
	docker build --build-arg "TEST_UID=$(id -u)" --build-arg "TEST_GID=$(id -g)" \
		--build-arg "DEBIAN_MIRROR=${DEBIAN_MIRROR:-deb.debian.org}" \
		-t "$image" "$repo/scripts/compat"
fi
docker run --rm --init --cap-drop ALL --security-opt no-new-privileges \
	--shm-size=1g --ulimit nofile=65536:65536 \
	--mount "type=bind,src=$repo,dst=/sdk,readonly" \
	--mount "type=bind,src=$work,dst=/work" \
	--mount "type=bind,src=$(go env GOROOT),dst=/opt/go,readonly" \
	-e "LTTNG_BUILD_JOBS=${LTTNG_BUILD_JOBS:-8}" \
	"$image" bash /sdk/scripts/compat/run.sh "$mode"
