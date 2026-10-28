# Runtime compatibility

The SDK builds against **2.14.x headers**, but dynamically loads
`liblttng-ctl.so.0`. Neither package links directly to that library. The same
binary targets **2.8.x, 2.10.x, 2.12.x, and 2.14.x**, on the same CPU architecture
with a compatible libc. Deployment machines need the runtime library, not its
headers, Go, or the binding generator.

## Library selection and errors

The first native operation loads the library using `dlopen(RTLD_NOW | RTLD_LOCAL)`.
The system loader search path, including `LD_LIBRARY_PATH`, applies. Set
`LTTNG_CTL_GO_LIBRARY` to an absolute shared-library path before the first call to
select an installation explicitly. Selection is process-wide and immutable,
including after a failed load. The library is never unloaded while native objects
may still exist. This environment variable does not select a session daemon.

- Failed loading returns `ErrLibraryUnavailable` in either package, with loader
  diagnostics (including missing dependencies).
- Missing functions return `ErrUnsupported`; `ctl.UnsupportedError.Symbol`
  identifies the missing API. No native operation is emulated as successful.
- `ctl.HasSymbol(ctx, "lttng_rotate_session")` checks library capabilities.
  It does not guarantee support by the daemon, tracer, or current credentials.
- Cancellation is checked before loading or dispatch, under the same shared lock
  as root-package and `ctl` operations. Native calls remain synchronous.
- Generated constructors check the availability of their result's destructor
  before calling C. Closing nil/already-closed objects remains a no-op.

The Go API and constants are the 2.14 API regardless of the runtime version.
Older libraries need not support newer enum values: native validation still
applies. Native error/status numbers are preserved, not renumbered. In particular,
2.10's `LTTNG_ERR_AGENT_TRACING_DISABLED` is **139**, while 2.14 uses **137**;
the `ctl.ErrAgentTracingDisabled` constant represents 2.14. Use the runtime's
`ctl.Strerror` for native diagnostic text. `LTTNG_ERR_NR` is a version-dependent
sentinel, not a portable error number.

## Recording compatibility and ownership

The root package uses older public APIs where needed:

- 2.8 channel defaults use a zero-initialized public channel and
  `lttng_channel_set_default_attr`; newer libraries use channel allocation APIs.
- Basic event rules use zero-initialized public events without extended fields.
- 2.8/2.10 session creation uses `lttng_create_session`. Separate control/data
  URLs are applied afterward, as in the upstream CLI. Setup failure destroys the
  newly created session and reports any cleanup failure too.
- `SessionOptions{}` continues to mean no output on every version.

Native daemon behavior is preserved. In particular, 2.12.17 sessiond returns
success when disabling a UST channel in an inactive session but leaves the channel
enabled (`channel_ust_disable` exits before updating its state). The matrix compares
this behavior directly with the official CLI, and separately verifies that channel
disable works while tracing is active. The SDK does not start sessions implicitly
to work around this upstream behavior.
Older daemons also reject channel enable calls after tracing has been started,
even when the channel already exists; native errors are returned unchanged.

The `ctl` package exposes exactly the native operations available on the selected
library. For example, `ctl.EventCreate` is unavailable on 2.8/2.10 even though the
root package can configure basic events on those versions.
Notification subscription is available on 2.10, while the pending-notification
query is absent there and returns `ctl.ErrUnsupported`.

Before 2.13, native triggers only borrow their condition/action. The Go wrapper
pins their owning resources until all dependent triggers close, including inputs
borrowed from another trigger. Explicitly closing an input still immediately
invalidates that input's Go wrapper. This avoids use-after-free while preserving
the same trigger lifetime behavior on newer libraries with native refcounting.

## ABI audit

`scripts/check-ctl-abi.py` compares shared function signatures, enum values, and
the sizes, alignment, and common field offsets of the public structures accessed
by the SDK. Opaque object layouts remain entirely private to the runtime library.
Function and global-variable bridges are generated from the 2.14 API inventory.

The older session structure has alignment 4 rather than 8 on x86-64, but retains
the same size, array stride, and accessed-field offsets. Native session arrays
come from `malloc`; their base and stride satisfy the newer alignment. This and
the two enum exceptions above are explicitly recorded in the audit. Unexpected
differences fail the check.

## Reproducible matrix

```sh
sh scripts/test-compat.sh all
# Reuse native builds and repeat the build-once/runtime matrix:
sh scripts/test-compat.sh test
```

The Linux x86-64 matrix pins upstream source commits for these combinations:

| LTTng-tools / liblttng-ctl | LTTng-UST |
| --- | --- |
| 2.8.8 | 2.8.4 |
| 2.10.11 | 2.10.7 |
| 2.12.17 | 2.12.10 |
| 2.14.0 | 2.14.0 |

It builds the root and `ctl` integration-test binaries **once**, against 2.14
headers, with race detection and `GOEXPERIMENT=cgocheck2`. It checks there are no
unresolved `lttng_*` references or `DT_NEEDED` entries for liblttng-ctl, and verifies
the identical SHA-256 hashes before every runtime run. Coverage includes recording,
filters/exclusions/context, list structure layouts, CLI interoperability, save/load,
snapshots, supported notifications/rotation objects, trigger ownership, and
explicit errors for missing APIs. The complete existing advanced suite also runs
on 2.14. The other patch releases and mixed-version library/daemon combinations
are not individually tested.

Builds and logs are retained in
`${TMPDIR:-$HOME/tmp}/opencode/lttng-ctl-go-compat` (`LTTNG_COMPAT_WORK` overrides).
`bin/SHA256SUMS` records the common binaries; `logs/test-*.log`, `logs/abi.log`,
and `logs/linkage.log` contain results. Docker, Git, flock, and Go are required.
The Debian 12 container runs tests as the invoking non-root UID, with private
PID/network namespaces and no Linux capabilities. Each runtime uses an isolated
UST sessiond; host daemons are untouched.

The harness builds liburcu 0.10.3 for the old UST symbol names and 0.14.3 for
LTTng-tools 2.14. For 2.8, compiler flags select glibc's `gettid` instead of the
conflicting legacy inline fallback; source files are not patched. `-fcommon`
accommodates old C sources. These build accommodations do not change the public
control API. Set `DEBIAN_MIRROR` to a Debian mirror hostname if needed; APT retains
upstream signature verification. `LTTNG_BUILD_JOBS` controls native build parallelism.

The container's libc baseline is not a promise of compatibility with older
operating systems. Build deployment binaries with a toolchain/sysroot matching
your oldest supported system; runtime LTTng version and libc compatibility are
separate constraints.
