#!/bin/sh
# Build the SDK once, then execute the same test binaries against each runtime.
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
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
while read -r series tools tools_commit ust ust_commit; do
	case "$series" in \#* | '') continue ;; esac
	for project in tools ust; do
		if [ "$project" = tools ]; then
			version=$tools
			commit=$tools_commit
		else
			version=$ust
			commit=$ust_commit
		fi
		source="$work/$project-$version"
		if [ ! -d "$source" ]; then
			GIT_TERMINAL_PROMPT=0 git -c advice.detachedHead=false clone --depth=1 --branch="v$version" \
				"https://github.com/lttng/lttng-$project.git" "$source"
		fi
		test "$(git -C "$source" rev-parse HEAD)" = "$commit"
		git -C "$source" diff --quiet
		git -C "$source" diff --cached --quiet
	done
done <"$repo/scripts/compat/versions.tsv"
# Old UST requires pre-0.11 symbols; LTTng-tools 2.14 requires liburcu >= 0.14.
while read -r version commit; do
	case "$version" in \#* | '') continue ;; esac
	if [ ! -d "$work/urcu-$version" ]; then
		GIT_TERMINAL_PROMPT=0 git -c advice.detachedHead=false clone --depth=1 --branch="v$version" \
			https://github.com/urcu/userspace-rcu.git "$work/urcu-$version"
	fi
	test "$(git -C "$work/urcu-$version" rev-parse HEAD)" = "$commit"
	git -C "$work/urcu-$version" diff --quiet
	git -C "$work/urcu-$version" diff --cached --quiet
done <"$repo/scripts/compat/urcu.tsv"
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
