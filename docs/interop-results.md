# LTTng interoperability results

## Environment

- LTTng-tools tag: `v2.14.0`.
- Unmodified source commit: `8644cbc50c40ff75168acbb9d3ccc814f1072305`.
- Fedora 44 x86-64 container; LTTng-UST 2.14.0, Babeltrace/bt2 2.1.2,
  Python 3.14.8, GCC 16.2.1, Go 1.27.1.
- Fedora base image used in this run:
  `registry.fedoraproject.org/fedora:44@sha256:797c691972d3d82481a7325b37dcce30db41b1e8661117b7dc7948052eb0cb0c`.
  The Dockerfile uses the Fedora 44 tag, so later rebuilds can pick up updates.
- Tests run as a non-root user with all capabilities dropped and private
  PID/network namespaces. No host daemon is stopped and destructive tests are
  not enabled.
- SDK tests link to `/work/install/lib/liblttng-ctl.so.0`, built from the pinned
  source, rather than a distribution-provided control library (`sdk-linkage.log`).

## Original upstream suite

The original suite establishes the native LTTng baseline; it does **not** invoke
the Go SDK or establish Go API coverage. Sources and assertions are unchanged.

The initial unit run passed **1089/1089** TAP results. The initial regression run
reported **337 total, 322 PASS, 13 SKIP, 1 FAIL, 1 ERROR**. Both unsuccessful
results came from `tools/config-directory/test_config.py`: the container's
`HOME=/work/home` disagreed with its passwd home `/home/tester`, invalidating two
default-path assertions. The ERROR was the serial runner's unsuccessful exit,
not a second independent failure. Keeping both home paths at `/home/tester`
fixed the environment; the unmodified script then passed all eight assertions.
Initial evidence is retained in `regression-initial.log` and `config-initial.log`.

After correcting HOME, the original top-level `make check` completed with exit
status **0**:

| Suite | TOTAL | PASS | SKIP | FAIL | ERROR |
| --- | ---: | ---: | ---: | ---: | ---: |
| Unit | 1089 | 1089 | 0 | 0 | 0 |
| Regression | 336 | 323 | 13 | 0 | 0 |
| Destructive | 44 | 0 | 44 | 0 | 0 |
| Meta | 0 | 0 | 0 | 0 | 0 |
| Perf | 0 | 0 | 0 | 0 | 0 |

XFAIL and XPASS were zero throughout. The regression total decreased from 337
to 336 because the serial runner no longer emitted an extra ERROR result.
The previously failing configuration script passed in the complete rerun.
Meta had no tests because ShellCheck was unavailable; perf had no tests because
libpfm support was not configured. Destructive cases remained gated off.
Evidence: `logs/full.log` and `build/tests/*/test-suite.log`.

Counts are Automake/TAP results, not necessarily individual scripts: unit tests
report individual assertions, while `tests.serial` reports one result per child
script and includes nested assertion/skip counts in its diagnostics. In
particular, an aggregate PASS can represent a child script whose assertions were
all skipped. Do not interpret the aggregate SKIP count as an exhaustive list of
unexecuted functionality.

## SDK-specific checks

`sh scripts/test-upstream.sh sdk` runs the SDK's full integration suite with
`GOEXPERIMENT=cgocheck2`, `-race`, and `-count=1` (no cached test results). The
following additional checks passed
against the upstream-built control library and tools:

| Check | Verified behavior |
| --- | --- |
| SDK to official CLI | CLI XML machine interface sees channel sizing, filter, exclusions, enabled state, and SDK stop/re-enable operations. SDK sees CLI start/disable/destroy operations. |
| Official CLI to SDK | SDK reads CLI-created channels, filters, exclusions, and log levels; CLI sees SDK start/channel-disable operations. |
| Cross-language save/load | CLI saves and SDK loads; SDK saves and CLI loads. Session existence is checked, not just return status. |
| Upstream UST fixture | Normal recording and snapshot, each with SDK-created and CLI-created sessions, using the unmodified `gen-ust-events` executable. |
| Actual trace data | Each of the four fixture combinations yields exactly five `tp:tptest` events, payload `intfield` values 5 through 9, and positive VTID, decoded through Python bt2. |
| Native C reference | Go load status and actual restored-session state agree with direct, unwrapped C calls for both a file and a directory. |

Fixture-dependent tests explicitly skip when the upstream generator is not
configured. The upstream harness sets its path so that all four combinations
execute. Go unit tests with `-race` and `go vet -tags=integration ./...` also pass.

## Observed upstream load behavior

For a saved **no-output** session, both native C and Go produced:

| Input | Native return | Session restored |
| --- | --- | --- |
| Explicit `.lttng` file | `-87` (`LTTNG_ERR_LOAD_INVALID_CONFIG`) | No |
| Directory containing that file | `0` | No |

This is an upstream 2.14.0 behavior, not a Go conversion discrepancy. The SDK
preserves the native result; the test compares both status and session existence
and does not pretend that the directory load restored the session. The positive
save/load test uses a local-output session and confirms that it really returns.

## Reproduction and limitations

```sh
sh scripts/test-upstream.sh full  # Original top-level make check
sh scripts/test-upstream.sh sdk   # Full SDK suite and Go/CLI/C interoperability
```

Artifacts are retained under
`${TMPDIR:-$HOME/tmp}/opencode/lttng-ctl-go-upstream-2.14.0`, or
`LTTNG_UPSTREAM_WORK` when explicitly set:

- `logs/`: stage output, versions, and SDK linkage.
- `build/tests/`: original per-test `.log`, `.trs`, and `test-suite.log` files.
- `source/`, `build/`, `install/`: pinned checkout, build tree, and native prefix.

Kernel tracing and destructive/root-only cases are intentionally not exercised.
The configured build lacks Java/Python-agent and libpfm support; optional suites
depend on upstream configure/environment gates. Stress scripts not registered
with `make check` are not run. These results do not establish runtime coverage of
every exported Go function or every notification payload. Upstream relayd tests
exercise native streaming, but this task does not add Go-controlled network
streaming interoperability coverage.
