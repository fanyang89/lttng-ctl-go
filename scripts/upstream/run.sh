#!/bin/bash
set -euo pipefail
mode=${1:-all}
commit=8644cbc50c40ff75168acbb9d3ccc814f1072305
mkdir -p /work/logs /work/go-cache /work/tmp
# Upstream's default-path tests derive HOME from getpwuid(), not getenv().
# Keep both consistent rather than changing or skipping those assertions.
export HOME=/home/tester TMPDIR=/work/tmp GOCACHE=/work/go-cache
unset LTTNG_ENABLE_DESTRUCTIVE_TESTS LTTNG_RUNDIR LTTNG_HOME LTTNG_UST_CTL_PATH LTTNG_UST_APP_PATH
test "$(git -C /work/source rev-parse HEAD)" = "$commit"
test -z "$(git -C /work/source status --porcelain)"
if [[ ! -f /work/source/configure ]]; then
	(cd /work/source && ./bootstrap) >/work/logs/bootstrap.log 2>&1
fi
mkdir -p /work/build /work/install
if [[ ! -f /work/build/Makefile ]]; then
	(cd /work/build && /work/source/configure --prefix=/work/install \
		--disable-man-pages --enable-python-bindings PYTHON=/usr/bin/python3) \
		>/work/logs/configure.log 2>&1
fi
make -C /work/build -j "${LTTNG_BUILD_JOBS:-8}" >/work/logs/build.log 2>&1
make -C /work/build install >/work/logs/install.log 2>&1
export PATH=/work/install/bin:/work/install/sbin:$PATH
export LD_LIBRARY_PATH=/work/install/lib:/work/install/lib64
export PKG_CONFIG_PATH=/work/install/lib/pkgconfig:/work/install/lib64/pkgconfig
export LTTNG_SESSION_CONFIG_XSD_PATH=/work/install/share/xml/lttng
{
	printf 'Source commit: %s\n' "$commit"
	lttng --version
	pkg-config --modversion lttng-ctl lttng-ust babeltrace2
	/usr/bin/python3 -c 'import sys, bt2; print(sys.version); print(bt2.__version__)'
	go version
} >/work/logs/versions.log

status=0
run_test() {
	local label=$1
	shift
	echo "Running $label (log: /work/logs/$label.log)"
	if "$@" >"/work/logs/$label.log" 2>&1; then
		echo "$label: PASS"
	else
		local result=$?
		echo "$label: FAIL (exit $result)"
		tail -n 60 "/work/logs/$label.log"
		status=1
	fi
}
if [[ "$mode" == unit || "$mode" == all ]]; then
	run_test unit make -C /work/build/tests/unit check
fi
if [[ "$mode" == regression || "$mode" == all ]]; then
	run_test regression make -C /work/build/tests/regression check
fi
if [[ "$mode" == full ]]; then
	run_test full make -C /work/build check
fi
if [[ "$mode" == config ]]; then
	run_test config bash -c 'cd /work/build/tests/regression && ./tools/config-directory/test_config.py'
fi
if [[ "$mode" == sdk || "$mode" == all ]]; then
	export LTTNG_UPSTREAM_EVENT_GENERATOR=/work/build/tests/utils/testapp/gen-ust-events/gen-ust-events
	run_test sdk bash -c 'cd /sdk && GOEXPERIMENT=cgocheck2 sh scripts/test-integration.sh -count=1 -race -v -timeout=180s'
	run_test sdk-linkage bash -c '
set -eu
cd /sdk
go test -tags=integration -c -o /work/sdk.test .
linkage=$(ldd /work/sdk.test)
printf "%s\n" "$linkage"
printf "%s\n" "$linkage" | grep -Eq "liblttng-ctl.so.0 => /work/install/lib(64)?/liblttng-ctl.so.0"
'
fi
exit "$status"
