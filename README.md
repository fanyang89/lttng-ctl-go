# lttng-go

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

UST and kernel domains are exposed; end-to-end recording is tested with UST.
Kernel tracing requires the appropriate modules and permissions and is not
covered by the current integration suite. Agent domains (Java/Python), probes,
snapshot/live session creation, rotation, trackers, triggers, and advanced
channel/context attributes are not wrapped in this initial API.

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
  isolation. Direct use of the C library by other code needs external coordination.
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

Upstream references: [LTTng-tools](https://github.com/lttng/lttng-tools),
[C headers](https://github.com/lttng/lttng-tools/tree/stable-2.14/include/lttng),
and [Python bindings](https://github.com/lttng/lttng-tools/tree/stable-2.14/extras/bindings/swig/python).
liblttng-ctl is licensed LGPL-2.1-only; review the upstream license and applicable
distribution requirements when shipping linked binaries.
