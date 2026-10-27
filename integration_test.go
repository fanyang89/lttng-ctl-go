//go:build integration && linux && cgo

package lttng

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("LTTNG_GO_INTEGRATION") != "1" {
		t.Skip("use sh scripts/test-integration.sh to start an isolated sessiond")
	}
	alive, err := SessionDaemonAlive(t.Context())
	if err != nil || !alive {
		t.Fatalf("isolated sessiond is not reachable: alive=%v, err=%v", alive, err)
	}
}

func createTestSession(t *testing.T, options SessionOptions) string {
	t.Helper()
	name := fmt.Sprintf("lttng-go-%d-%s", os.Getpid(), t.Name())
	name = strings.ReplaceAll(name, "/", "-")
	if err := CreateSession(t.Context(), name, options); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// t.Context is canceled before cleanup; cleanup must use a fresh context.
		if err := DestroySession(context.Background(), name); err != nil && !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("cleanup session: %v", err)
		}
	})
	return name
}

func TestIntegrationLifecycle(t *testing.T) {
	requireIntegration(t)
	ctx := t.Context()
	ust := Domain{Type: DomainUST, BufferType: BufferPerUID}
	name := createTestSession(t, SessionOptions{})
	if err := CreateSession(ctx, name, SessionOptions{}); !errors.Is(err, ErrSessionExists) {
		t.Fatalf("duplicate session: %v", err)
	} else if native, ok := errors.AsType[*Error](err); !ok || native.Message == "" {
		t.Fatalf("missing native error: %v", err)
	}
	config, err := DefaultChannelConfig(ctx, ust)
	if err != nil {
		t.Fatal(err)
	}
	config.SubbufferSize, config.SubbufferCount = 4096, 2
	config.ReadTimer, config.SwitchTimer = 10*time.Millisecond, 20*time.Millisecond
	config.TracefileSize, config.TracefileCount = 65536, 3
	if err := EnableChannel(ctx, name, ust, "events", &config); err != nil {
		t.Fatal(err)
	}
	if err := AddContext(ctx, name, ust, "events", ContextVTID); err != nil {
		t.Fatal(err)
	}
	if err := EnableEvent(ctx, name, ust, "events", EventRule{Filter: "msg =="}); err == nil {
		t.Fatal("native filter compiler accepted an invalid expression")
	} else if _, ok := errors.AsType[*Error](err); !ok {
		t.Fatalf("filter compilation did not preserve native error: %v", err)
	}
	rule := EventRule{Name: "lttng_ust_*", Filter: "$ctx.vtid > 0", Exclusions: []string{"lttng_ust_statedump:*"}, LogLevelType: LogLevelRange, LogLevel: 14}
	if err := EnableEvent(ctx, name, ust, "events", rule); err != nil {
		t.Fatal(err)
	}
	domains, err := ListDomains(ctx, name)
	if err != nil || len(domains) != 1 || domains[0] != ust {
		t.Fatalf("domains: %+v, %v", domains, err)
	}
	channels, err := ListChannels(ctx, name, ust)
	if err != nil || len(channels) != 1 || !channels[0].Enabled || channels[0].Name != "events" {
		t.Fatalf("channels: %+v, %v", channels, err)
	}
	wantConfig := config
	wantConfig.LossMode = LossDiscard // session default resolves to discard
	if channels[0].Config != wantConfig {
		t.Fatalf("channel configuration: got %+v, want %+v", channels[0].Config, wantConfig)
	}
	events, err := ListEvents(ctx, name, ust, "events")
	if err != nil || len(events) != 1 || !events[0].Enabled || events[0].Filter != rule.Filter || !slices.Equal(events[0].Exclusions, rule.Exclusions) {
		t.Fatalf("events: %+v, %v", events, err)
	}
	if events[0].Name != rule.Name || events[0].LogLevelType != rule.LogLevelType || events[0].LogLevel != rule.LogLevel {
		t.Fatalf("rule did not round trip: %+v", events[0])
	}
	if err := DisableEvent(ctx, name, ust, "events", rule.Name); err != nil {
		t.Fatal(err)
	}
	events, err = ListEvents(ctx, name, ust, "events")
	if err != nil || len(events) != 1 || events[0].Enabled {
		t.Fatalf("event was not disabled: %+v, %v", events, err)
	}
	if err := EnableEvent(ctx, name, ust, "events", rule); err != nil {
		t.Fatal(err)
	}
	if err := DisableChannel(ctx, name, ust, "events"); err != nil {
		t.Fatal(err)
	}
	channels, err = ListChannels(ctx, name, ust)
	if err != nil || len(channels) != 1 || channels[0].Enabled {
		t.Fatalf("channel was not disabled: %+v, %v", channels, err)
	}
	if err := EnableChannel(ctx, name, ust, "events", nil); err != nil {
		t.Fatal(err)
	}
	if err := StartSession(ctx, name); err != nil {
		t.Fatal(err)
	}
	sessions, err := ListSessions(ctx)
	if err != nil || !slices.ContainsFunc(sessions, func(s Session) bool { return s.Name == name && s.Enabled }) {
		t.Fatalf("active session not found: %+v, %v", sessions, err)
	}
	if err := StartSession(ctx, name); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("start twice: %v", err)
	}
	if err := StopSessionNoWait(ctx, name); err != nil {
		t.Fatal(err)
	}
	if pending, err := DataPending(ctx, name); err != nil || pending {
		t.Fatalf("no-output session pending: %v, %v", pending, err)
	}
	if err := StartSession(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := StopSession(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := DestroySession(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := StartSession(ctx, name); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("start destroyed session: %v", err)
	}
	// Copied values remain valid after every native allocation and session is gone.
	if events[0].Filter != rule.Filter || events[0].Exclusions[0] != rule.Exclusions[0] {
		t.Fatal("returned event memory did not survive native destruction")
	}
}

func TestIntegrationConcurrentCalls(t *testing.T) {
	requireIntegration(t)
	ctx := t.Context()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			name := fmt.Sprintf("lttng-go-concurrent-%d-%d", os.Getpid(), i)
			if err := CreateSession(ctx, name, SessionOptions{}); err != nil {
				t.Error(err)
				return
			}
			defer func() {
				if err := DestroySession(context.Background(), name); err != nil {
					t.Error(err)
				}
			}()
			for range 10 {
				sessions, err := ListSessions(ctx)
				if err != nil || !slices.ContainsFunc(sessions, func(s Session) bool { return s.Name == name }) {
					t.Errorf("concurrent list: %+v, %v", sessions, err)
				}
			}
		})
	}
	wg.Wait()
}

func TestIntegrationDefaultsAndCanceledMutation(t *testing.T) {
	requireIntegration(t)
	ctx := t.Context()
	ust := Domain{Type: DomainUST, BufferType: BufferPerPID}
	name := createTestSession(t, SessionOptions{})
	domains, err := ListDomains(ctx, name)
	if err != nil || len(domains) != 0 {
		t.Fatalf("empty session domains: %+v, %v", domains, err)
	}
	if err := EnableEvent(ctx, name, ust, "", EventRule{}); err != nil {
		t.Fatal(err)
	}
	channels, err := ListChannels(ctx, name, ust)
	if err != nil || len(channels) != 1 || channels[0].Name != "channel0" {
		t.Fatalf("implicit default channel: %+v, %v", channels, err)
	}
	events, err := ListEvents(ctx, name, ust, "channel0")
	if err != nil || len(events) != 1 || events[0].Name != "*" || events[0].Filter != "" || len(events[0].Exclusions) != 0 {
		t.Fatalf("zero-valued rule: %+v, %v", events, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := StartSession(canceled, name); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start: %v", err)
	}
	sessions, err := ListSessions(ctx)
	if err != nil || !slices.ContainsFunc(sessions, func(s Session) bool { return s.Name == name && !s.Enabled && s.Path == "" }) {
		t.Fatalf("canceled start mutated the no-output session: %+v, %v", sessions, err)
	}
	if _, err := ListEvents(ctx, name+"-missing", ust, "channel0"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("list nonexistent session: %v", err)
	}
}

func TestIntegrationUnavailableDaemon(t *testing.T) {
	if os.Getenv("LTTNG_GO_MISSING_DAEMON") == "1" {
		alive, err := SessionDaemonAlive(t.Context())
		if err != nil || alive {
			t.Fatalf("unexpected daemon connection: alive=%v, err=%v", alive, err)
		}
		if sessions, err := ListSessions(t.Context()); !errors.Is(err, ErrNoSessiond) || sessions != nil {
			t.Fatalf("unavailable daemon: sessions=%+v, err=%v", sessions, err)
		}
		return
	}
	requireIntegration(t)
	// Re-exec so the C library sees this configuration before initialization.
	missing := filepath.Join(t.TempDir(), "missing")
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestIntegrationUnavailableDaemon$", "-test.v")
	child.Env = append(os.Environ(), "LTTNG_GO_MISSING_DAEMON=1", "LTTNG_HOME="+missing, "LTTNG_RUNDIR="+missing)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("missing-daemon subprocess: %v\n%s", err, output)
	}
}

func TestIntegrationRecordsFilteredTrace(t *testing.T) {
	requireIntegration(t)
	ctx := t.Context()
	dir := t.TempDir()
	binary := filepath.Join(dir, "emitter")
	compile := exec.CommandContext(ctx, "cc", "testdata/emitter.c", "-o", binary, "-llttng-ust", "-ldl")
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile emitter: %v\n%s", err, output)
	}
	emitter := exec.CommandContext(ctx, binary)
	emitter.Env = append(os.Environ(), "LTTNG_UST_REGISTER_TIMEOUT=-1")
	stdin, err := emitter.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := emitter.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = emitter.Process.Kill() })
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("emitter readiness: %q, %v", line, err)
	}
	ust := Domain{Type: DomainUST, BufferType: BufferPerUID}
	points, err := ListTracepoints(ctx, ust)
	if err != nil || !slices.ContainsFunc(points, func(e Event) bool { return e.Name == "lttng_ust_tracef:event" && e.PID == emitter.Process.Pid }) {
		t.Fatalf("emitter tracepoint missing: %+v, %v", points, err)
	}
	tracePath := filepath.Join(dir, "trace")
	name := createTestSession(t, SessionOptions{OutputPath: tracePath})
	config, err := DefaultChannelConfig(ctx, ust)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the per-CPU shared-memory footprint small on large CI hosts.
	config.SubbufferSize, config.SubbufferCount = 4096, 2
	if err := EnableChannel(ctx, name, ust, "events", &config); err != nil {
		t.Fatal(err)
	}
	if err := AddContext(ctx, name, ust, "events", ContextVTID); err != nil {
		t.Fatal(err)
	}
	if err := EnableEvent(ctx, name, ust, "events", EventRule{Name: "lttng_ust_tracef:event", Filter: `msg == "lttng-go accepted"`}); err != nil {
		t.Fatal(err)
	}
	if err := StartSession(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	if err := emitter.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := StopSession(ctx, name); err != nil {
		t.Fatal(err)
	}
	if pending, err := DataPending(ctx, name); err != nil || pending {
		t.Fatalf("stopped trace pending: %v, %v", pending, err)
	}
	if err := DestroySession(ctx, name); err != nil {
		t.Fatal(err)
	}
	output, err := exec.CommandContext(ctx, "babeltrace2", tracePath).CombinedOutput()
	if err != nil {
		t.Fatalf("read trace: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "lttng-go accepted") || strings.Contains(text, "lttng-go rejected") || !strings.Contains(text, "vtid") {
		t.Fatalf("unexpected filtered trace:\n%s", output)
	}
}
