#!/bin/bash
# Each build and runtime deliberately gets an isolated library search path.
# shellcheck disable=SC2030,SC2031
set -euo pipefail
mode=${1:-all}
case "$mode" in build | test | all) ;; *)
	echo 'Expected build, test, or all' >&2
	exit 2
	;;
esac
sdk=$(cd "$(dirname "$0")/../.." && pwd)
work=${LTTNG_COMPAT_WORK:-/work}
native=${LTTNG_COMPAT_NATIVE:-$work}
export LTTNG_COMPAT_WORK="$work"
export TMPDIR="$work/tmp" GOCACHE="$work/go-cache"
mkdir -p "$TMPDIR" "$GOCACHE" "$work/logs" "$work/bin"
cd "$sdk"
unset LTTNG_HOME LTTNG_RUNDIR LTTNG_UST_CTL_PATH LTTNG_UST_APP_PATH LTTNG_CTL_GO_LIBRARY

build_native() (
	tools=$1 ust=$2
	prefix=$native/install-$tools
	export PATH="$prefix/bin:$prefix/sbin:$PATH"
	export LD_LIBRARY_PATH="$prefix/lib:$prefix/lib64"
	export PKG_CONFIG_PATH="$prefix/lib/pkgconfig:$prefix/lib64/pkgconfig"
	export CPPFLAGS="-I$prefix/include"
	export LDFLAGS="-L$prefix/lib -L$prefix/lib64"
	if [[ $tools != 2.12.* ]]; then
		urcu=0.10.3
		if [[ $tools == 2.14.* ]]; then urcu=0.14.3; fi
		source=$work/urcu-$urcu
		[[ -f $source/configure ]] || (cd "$source" && ./bootstrap)
		mkdir -p "$work/build-urcu-$tools"
		(
			cd "$work/build-urcu-$tools"
			[[ -f Makefile ]] || "$source/configure" --prefix="$prefix" || exit
			make -j "${LTTNG_BUILD_JOBS:-8}" || exit
			make install
		) >"$work/logs/build-urcu-$tools.log" 2>&1 || {
			tail -n 60 "$work/logs/build-urcu-$tools.log"
			exit 1
		}
	fi
	if [[ $tools == 2.8.* ]]; then
		# glibc 2.30 added gettid; use it instead of the old inline fallback.
		export CPPFLAGS="$CPPFLAGS -D_GNU_SOURCE -D_LTTNG_UST_TID_H -DLTTNG_TID_H -include unistd.h"
	fi
	for project in ust tools; do
		if [[ $project == ust ]]; then version=$ust; else version=$tools; fi
		source=$work/$project-$version
		build=$work/build-$project-$version
		log=$work/logs/build-$project-$version.log
		echo "Building $project $version ($log)"
		(
			if [[ ! -f $source/configure ]]; then (cd "$source" && ./bootstrap); fi
			mkdir -p "$build"
			cd "$build"
			"$source/configure" --prefix="$prefix" --disable-man-pages CFLAGS='-O2 -g -fcommon' || exit
			make -j "${LTTNG_BUILD_JOBS:-8}" || exit
			make install
		) >"$log" 2>&1 || {
			tail -n 60 "$log"
			exit 1
		}
	done
)

if [[ $mode == build || $mode == all ]]; then
	while read -r series tools _tools_commit ust _ust_commit; do
		[[ $series == \#* || -z $series ]] && continue
		build_native "$tools" "$ust"
	done <"$sdk/scripts/compat/versions.tsv"
fi
[[ $mode != build ]] || exit 0

# Only this section compiles the Go SDK. Runtime tests below never invoke Go.
if [[ -n ${LTTNG_COMPAT_NATIVE:-} ]]; then
	cmp "$sdk/scripts/compat/versions.tsv" "$native/versions.tsv"
	cmp "$sdk/scripts/compat/urcu.tsv" "$native/urcu.tsv"
fi
export CGO_CPPFLAGS=-I$native/install-2.14.0/include
export GOEXPERIMENT=cgocheck2
go test -race -tags=integration -c -o "$work/bin/root.test" .
go test -race -tags=integration -c -o "$work/bin/ctl.test" ./ctl
(cd "$work/bin" && sha256sum root.test ctl.test >SHA256SUMS)
readelf -d "$work/bin/root.test" "$work/bin/ctl.test" >"$work/logs/linkage.log"
nm -u "$work/bin/root.test" "$work/bin/ctl.test" >"$work/logs/undefined-symbols.log"
if grep -q 'NEEDED.*liblttng-ctl' "$work/logs/linkage.log" ||
	grep -q ' lttng_' "$work/logs/undefined-symbols.log"; then
	echo 'SDK still has a link-time dependency on LTTng' >&2
	exit 1
fi
python3 scripts/check-ctl-abi.py --baseline "$native/install-2.14.0/include" \
	"$native/install-2.8.8/include" "$native/install-2.10.11/include" "$native/install-2.12.17/include" \
	>"$work/logs/abi.log"

status=0
while read -r series tools _tools_commit ust _ust_commit; do
	[[ $series == \#* || -z $series ]] && continue
	echo "Testing unchanged binaries against LTTng $tools / UST $ust"
	(cd "$work/bin" && sha256sum -c SHA256SUMS)
	(
		prefix=$native/install-$tools
		export PATH="$prefix/bin:$prefix/sbin:$PATH"
		export LD_LIBRARY_PATH="$prefix/lib:$prefix/lib64"
		export PKG_CONFIG_PATH="$prefix/lib/pkgconfig:$prefix/lib64/pkgconfig"
		export CPATH="$prefix/include"
		export LIBRARY_PATH="$prefix/lib:$prefix/lib64"
		export LTTNG_CTL_GO_LIBRARY="$prefix/lib/liblttng-ctl.so.0"
		export LTTNG_COMPAT_SERIES=$series
		export LTTNG_SESSION_CONFIG_XSD_PATH="$prefix/share/xml/lttng"
		lttng --version
		pkg-config --modversion lttng-ctl lttng-ust
		bash "$sdk/scripts/compat/runtime.sh" "$series"
	) >"$work/logs/test-$tools.log" 2>&1 || {
		tail -n 80 "$work/logs/test-$tools.log"
		status=1
	}
done <"$sdk/scripts/compat/versions.tsv"
[[ $status == 0 ]] || exit "$status"
echo "Compatibility matrix passed. Binary hashes and logs: $work/bin $work/logs"
