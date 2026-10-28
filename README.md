# lttng-ctl-go

A thin, idiomatic Go wrapper around the official **liblttng-ctl** C API.
It controls a local LTTng session daemon using CGO, without spawning `lttng`
commands or reimplementing LTTng's private protocol. This is not an official
LTTng project.

## Requirements

- Linux and Go 1.27.1 or newer (as specified in `go.mod`).
- CGO enabled, a C compiler, and `pkg-config`.
- liblttng-ctl development headers and shared library. Validated with LTTng
  **2.14.0**; other versions are not currently validated.
- A running session daemon for control operations. The SDK never launches one.

For Debian/Ubuntu, install `build-essential pkg-config liblttng-ctl-dev
lttng-tools`. For Fedora, install `gcc pkgconf-pkg-config lttng-tools-devel
lttng-tools`. Package names may vary by distribution.

```sh
pkg-config --modversion lttng-ctl
CGO_ENABLED=1 go build ./...
```

Consumers also need the liblttng-ctl shared library and its dependencies at
runtime. CGO complicates cross-compilation: use a C toolchain, headers, and
libraries for the target platform. There is no pure-Go fallback.

## API

| Area | Functions |
| --- | --- |
| Session | `CreateSession`, `ListSessions`, `StartSession`, `StopSession`, `StopSessionNoWait`, `DataPending`, `DestroySession` |
| Discovery | `SessionDaemonAlive`, `ListDomains`, `ListTracepoints` |
| Channel | `DefaultChannelConfig`, `EnableChannel`, `DisableChannel`, `ListChannels` |
| Event rules | `EnableEvent`, `DisableEvent`, `ListEvents` |
| Context | `AddContext` for basic fields such as VTID, VPID, and process name |

Normal sessions support local output directories, network output to an LTTng
relay daemon, or no output. **`SessionOptions{}` means no output**, not the CLI's
default trace directory. Network output URLs are not remote sessiond endpoints.

For the **complete liblttng-ctl 2.14 API**, import
`github.com/fanyang89/lttng-ctl-go/ctl`. The root package,
`github.com/fanyang89/lttng-ctl-go` (package `lttng`), remains the simpler
recording API described above. The `ctl` package covers **all 425 public C
functions**, all public enums, and the two global endpoints, including deprecated
APIs. See [the complete symbol/signature inventory](ctl/API.tsv).

The complete layer includes snapshot/live descriptors and outputs, rotation and
schedules, process-attribute trackers, save/load, asynchronous clear/destruction,
triggers, actions, rate policies, conditions, notification channels, evaluations,
captured values, error queries, agent event rules, probes, health, extended channel
attributes, and perf/application context fields. No unsupported native operation
is replaced by a stub.

`ctl` retains native scalar/status values followed by converted output parameters
and an `error`. Status namespaces are typed separately; inspect `ctl.StatusError`
for native failures. Nullable C strings use `*string` (`nil` for NULL, `new("")`
for an empty string). All functions take a context first. Owned objects and array
`List`s need explicit `Close`; borrowed objects reject use after owner closure or
mutation, and const borrows reject writes. Ownership-transfer functions invalidate
the moved input on success (including snapshot action outputs and expression
trees). See [package documentation](ctl/doc.go) for these low-level contracts.

For example, constructing a snapshot session without an initial output:

```go
descriptor, err := ctl.SessionDescriptorSnapshotCreate(ctx, new("snapshot"))
if err != nil { return err }
defer descriptor.Close()
_, err = ctl.CreateSessionExt(ctx, descriptor)
// Handle err; this creates a daemon-side session which must later be destroyed.
```

Plain C configuration structures have typed Go `Data`/`SetData` accessors and
constructors such as `NewDomain`, `NewEventContext`, and `NewChannelAttr`.
Names and filter semantics still follow upstream preconditions. Kernel operations
require modules/privileges; Java/Python tracing requires the relevant applications.
Binding coverage does not mean every deployment-dependent behavior is tested.

## Usage

Start a session daemon separately if needed:

```sh
lttng-sessiond --daemonize
```

The [complete recording example](examples/record/main.go) creates a session,
configures its channel and VTID context, installs an event rule, and starts
recording. Ctrl+C stops, drains, and destroys the session without deleting traces.

```sh
go run ./examples/record -output ./traces -event 'my_provider:*'
```

The central configuration calls are:

```go
domain := lttng.Domain{
    Type:       lttng.DomainUST,
    BufferType: lttng.BufferPerUID,
}

err := lttng.EnableEvent(ctx, "my-session", domain, "events", lttng.EventRule{
    Name:       "my_provider:*",
    Filter:     "value > 100 && $ctx.vtid > 0",
    Exclusions: []string{"my_provider:verbose"},
})
// Handle err. The session and channel must already exist.
```

Filters are compiled by liblttng-ctl. Empty event names match `*`; empty filters
mean no filtering. Exclusions require UST wildcard rules and follow LTTng's
native restrictions. `DisableEvent` disables matching variants regardless of
their filters or log levels. `ListEvents` requires an explicit channel name.

Pass `nil` to `EnableChannel` for official defaults. To customize attributes,
obtain `DefaultChannelConfig`, modify it, and pass its address. A non-nil config
is a complete set of basic attributes, not a sparse override: its zero value is
not valid. Sizes are bytes; timers are `time.Duration` values representable as
whole microseconds. LTTng enforces additional tracer-specific constraints.

## Errors, concurrency, and cancellation

Native failures retain their positive LTTng code and copied upstream message:

```go
if errors.Is(err, lttng.ErrSessionExists) {
    // Decide whether to reuse the existing session; the SDK does not retry.
}
if native, ok := errors.AsType[*lttng.Error](err); ok {
    log.Printf("%s: code=%d: %s", native.Op, native.Code, native.Message)
}
```

Input validation rejects embedded NUL bytes, oversized names, and values that
would truncate during C conversion; these errors wrap `ErrInvalid`. Native codes
not included among the named constants remain available as `ErrorCode` values.

- **Concurrent calls are serialized package-wide.** liblttng-ctl 2.14 uses
  process-global connection state. Separate Go objects would not provide
  isolation. Root and `ctl` calls share the same lock. Direct use of the C library
  by other code needs external coordination. A blocking notification receive
  also holds this lock: poll for pending notifications first or use a dedicated
  process for blocking reception.
- **Contexts cancel queued calls, not in-flight C calls.** Once a native call has
  begun, the SDK waits for its actual result, even if the context expires. It
  never returns early while an invisible background mutation continues, retries
  a command automatically, or claims that cancellation rolled back a mutation.
- `StopSession` and `DestroySession` can wait for data consumption. For
  caller-controlled polling, use `StopSessionNoWait` followed by `DataPending`
  with a timer/context between queries. Each individual query is still a
  synchronous native call. The session must remain stopped while draining.
- All returned strings, slices, and summaries own Go memory. C allocations are
  freed internally, so there are no `Close` methods or finalizers. Destroying a
  session is an explicit daemon-side operation, not a Go object cleanup.
- Connection selection follows liblttng-ctl's Unix credentials, tracing group,
  and environment. Configure `LTTNG_HOME`/`LTTNG_RUNDIR` before starting the
  process, and do not mutate process-wide configuration concurrently. There is
  no per-client socket or credential option. A relay daemon receives trace data,
  not session management commands.

## Tests

Unit tests do not require a session daemon:

```sh
go test -race ./...
go vet ./...
```

Integration tests additionally require `lttng-sessiond`, liblttng-ust development
headers/library, and `babeltrace2` (`liblttng-ust-dev`/`babeltrace2` on Debian,
`lttng-ust-devel`/`babeltrace2` on Fedora).

```sh
sh scripts/test-integration.sh -race -v -timeout=90s
```

The script starts its own isolated session daemon with separate runtime and UST
registration paths and no kernel tracing. It never stops an existing daemon.
Tests cover lifecycle operations, configuration round trips, filter/exclusion
ownership, concurrent control calls, and real UST recording read back through
Babeltrace to check filter and context results. The library itself never runs
external commands; only test fixtures and this test harness do.

Every one of the 425 complete-layer functions has a generated cancellation test
and is compiled and linked against the native library. Additional native tests
cover object families, const/borrowed/moved lifetimes, and configuration. Advanced
daemon tests exercise trackers, schedules, clear/destruction completion, trigger
registration, subscriptions, error queries, and save/load. Kernel recording,
relayd-dependent streaming, and every possible notification/evaluation payload
are not exhaustively exercised by this local UST suite.

Regenerate/check bindings from installed 2.14.x headers with Python 3 and gofmt:

```sh
go generate ./ctl
python3 scripts/generate-ctl.py --check
# Optional independent declaration audit (requires clang):
python3 scripts/check-ctl-api.py
```

Generated source is checked in, so consumers do not need Python or clang. Header
drift is detected rather than silently excluding declarations. Generation uses
explicit ownership exceptions for transferred objects and native-owned results.

### Official suite and interoperability

The upstream harness builds the unmodified LTTng-tools **v2.14.0** commit
`8644cbc50c40ff75168acbb9d3ccc814f1072305` in a Fedora 44 container. It runs as
the invoking non-root UID, with private PID/network namespaces and no Linux
capabilities. It does not use or stop host daemons, load kernel modules, or enable
destructive tests. Docker, Git, flock, and the project's Go toolchain are needed;
build dependencies including Python `bt2` are installed only inside the image.

```sh
# Original unit + regression suites, then Go tests against the built library:
sh scripts/test-upstream.sh all
# Literal upstream top-level make check, including applicable optional suites:
sh scripts/test-upstream.sh full
# Go/CLI/C interoperability using the upstream-built tools and UST generator:
sh scripts/test-upstream.sh sdk
```

Source, build products, and logs are retained under
`${TMPDIR:-$HOME/tmp}/opencode/lttng-ctl-go-upstream-2.14.0` (override with
`LTTNG_UPSTREAM_WORK`). Raw TAP/Automake logs remain in `build/tests`; stage logs
are in `logs`, including the native library linkage used by the Go test binary.
Failures are not suppressed, and SKIP is not counted as PASS. Kernel, destructive,
and unavailable agent tests remain subject to upstream's environment gates.

The original suite tests LTTng itself, not the Go wrapper. The additional Go tests
exercise SDK-created sessions through the official CLI and CLI-created sessions
through the SDK, XML machine-interface configuration checks, cross-language
save/load, and actual normal/snapshot recording of the **original upstream UST
generator**. Python `bt2` checks exact filtered payload values, counts, and VTID.
Fixture-dependent cases explicitly skip outside the upstream harness.

`TestInteropNoOutputLoadMatchesNative` also compares load errors and actual
session existence with a small, unwrapped C reference. This catches upstream
"success without restoration" behavior instead of accepting a zero return code
as proof of interoperability. See the test's verbose output for the result on
the library version being tested. Recorded results and coverage limits are in
[the interoperability report](docs/interop-results.md).

Upstream references: [LTTng-tools](https://github.com/lttng/lttng-tools),
[C headers](https://github.com/lttng/lttng-tools/tree/stable-2.14/include/lttng),
and [Python bindings](https://github.com/lttng/lttng-tools/tree/stable-2.14/extras/bindings/swig/python).
liblttng-ctl is licensed LGPL-2.1-only; review the upstream license and applicable
distribution requirements when shipping linked binaries.
