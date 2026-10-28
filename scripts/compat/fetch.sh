#!/bin/sh
# Fetch and verify the pinned native sources for local and image builds.
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
work=${LTTNG_COMPAT_WORK:-/work}
mkdir -p "$work"
fetch() {
	url=$1 version=$2 commit=$3 source=$4
	if [ ! -d "$source" ]; then
		GIT_TERMINAL_PROMPT=0 git -c advice.detachedHead=false clone --depth=1 \
			--branch="v$version" "$url" "$source"
	fi
	test "$(git -C "$source" rev-parse HEAD)" = "$commit"
	git -C "$source" diff --quiet
	git -C "$source" diff --cached --quiet
}
while read -r series tools tools_commit ust ust_commit; do
	case "$series" in \#* | '') continue ;; esac
	fetch https://github.com/lttng/lttng-tools.git "$tools" "$tools_commit" "$work/tools-$tools"
	fetch https://github.com/lttng/lttng-ust.git "$ust" "$ust_commit" "$work/ust-$ust"
done <"$repo/scripts/compat/versions.tsv"
while read -r version commit; do
	case "$version" in \#* | '') continue ;; esac
	fetch https://github.com/urcu/userspace-rcu.git "$version" "$commit" "$work/urcu-$version"
done <"$repo/scripts/compat/urcu.tsv"
