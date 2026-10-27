//go:build linux && cgo

package lttng

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestError(t *testing.T) {
	err := &Error{Op: "create session", Code: ErrSessionExists, Message: "Session already exists"}
	if !errors.Is(err, ErrSessionExists) || errors.Is(err, ErrNoSessiond) {
		t.Fatalf("unexpected error matching: %v", err)
	}
	if got, ok := errors.AsType[*Error](err); !ok || got.Code != ErrSessionExists {
		t.Fatalf("native error metadata lost: %v", err)
	}
	if !strings.Contains(err.Error(), "create session: Session already exists") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestValidation(t *testing.T) {
	ctx := t.Context()
	ust := Domain{Type: DomainUST, BufferType: BufferPerUID}
	validConfig, err := DefaultChannelConfig(ctx, ust)
	if err != nil {
		t.Fatal(err)
	}
	badTimer := validConfig
	badTimer.ReadTimer = time.Nanosecond
	badSize := validConfig
	badSize.SubbufferSize = 3
	badCount := validConfig
	badCount.SubbufferCount = 0
	badLoss := validConfig
	badLoss.LossMode = 99
	badOutput := validConfig
	badOutput.Output = 99
	negativeTimer := validConfig
	negativeTimer.SwitchTimer = -time.Microsecond
	overflowTimer := validConfig
	overflowTimer.SwitchTimer = (1 << 32) * time.Microsecond
	tests := []struct {
		name string
		call func() error
	}{
		{"empty session", func() error { return CreateSession(ctx, "", SessionOptions{}) }},
		{"NUL session", func() error { return StartSession(ctx, "a\x00b") }},
		{"long session", func() error { return DestroySession(ctx, strings.Repeat("x", 4096)) }},
		{"relative path", func() error { return CreateSession(ctx, "s", SessionOptions{OutputPath: "relative"}) }},
		{"mixed output", func() error {
			return CreateSession(ctx, "s", SessionOptions{OutputPath: "/trace", ControlURL: "net://localhost"})
		}},
		{"data without control", func() error { return CreateSession(ctx, "s", SessionOptions{DataURL: "tcp://localhost"}) }},
		{"NUL path", func() error { return CreateSession(ctx, "s", SessionOptions{OutputPath: "/trace\x00suffix"}) }},
		{"unknown domain", func() error { return EnableChannel(ctx, "s", Domain{}, "c", nil) }},
		{"kernel buffer type", func() error { return EnableChannel(ctx, "s", Domain{Type: DomainKernel}, "c", nil) }},
		{"UST buffer type", func() error {
			return EnableChannel(ctx, "s", Domain{Type: DomainUST, BufferType: BufferGlobal}, "c", nil)
		}},
		{"empty channel", func() error { return EnableChannel(ctx, "s", ust, "", nil) }},
		{"NUL channel", func() error { return DisableChannel(ctx, "s", ust, "c\x00x") }},
		{"long channel", func() error { return EnableChannel(ctx, "s", ust, strings.Repeat("x", 4096), nil) }},
		{"fractional timer", func() error { return EnableChannel(ctx, "s", ust, "c", &badTimer) }},
		{"negative timer", func() error { return EnableChannel(ctx, "s", ust, "c", &negativeTimer) }},
		{"overflow timer", func() error { return EnableChannel(ctx, "s", ust, "c", &overflowTimer) }},
		{"non power of two", func() error { return EnableChannel(ctx, "s", ust, "c", &badSize) }},
		{"zero count", func() error { return EnableChannel(ctx, "s", ust, "c", &badCount) }},
		{"loss mode", func() error { return EnableChannel(ctx, "s", ust, "c", &badLoss) }},
		{"output type", func() error { return EnableChannel(ctx, "s", ust, "c", &badOutput) }},
		{"NUL filter", func() error { return EnableEvent(ctx, "s", ust, "c", EventRule{Filter: "a\x00b"}) }},
		{"syscall in UST", func() error { return EnableEvent(ctx, "s", ust, "c", EventRule{Type: EventSyscall}) }},
		{"unsupported probe", func() error { return EnableEvent(ctx, "s", ust, "c", EventRule{Type: 2}) }},
		{"invalid log level", func() error {
			return EnableEvent(ctx, "s", ust, "c", EventRule{LogLevelType: LogLevelSingle, LogLevel: 15})
		}},
		{"invalid log rule", func() error { return EnableEvent(ctx, "s", ust, "c", EventRule{LogLevelType: 99}) }},
		{"empty exclusion", func() error { return EnableEvent(ctx, "s", ust, "c", EventRule{Exclusions: []string{""}}) }},
		{"NUL event", func() error { return DisableEvent(ctx, "s", ust, "c", "e\x00x") }},
		{"listing empty channel", func() error { _, err := ListEvents(ctx, "s", ust, ""); return err }},
		{"unsupported context", func() error { return AddContext(ctx, "s", ust, "c", ContextType(999)) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
		})
	}
}

func TestCanceledCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ust := Domain{Type: DomainUST, BufferType: BufferPerUID}
	calls := []func() error{
		func() error { _, err := SessionDaemonAlive(ctx); return err },
		func() error { _, err := ListSessions(ctx); return err },
		func() error { return CreateSession(ctx, "s", SessionOptions{}) },
		func() error { return StartSession(ctx, "s") },
		func() error { return StopSession(ctx, "s") },
		func() error { return StopSessionNoWait(ctx, "s") },
		func() error { _, err := DataPending(ctx, "s"); return err },
		func() error { return DestroySession(ctx, "s") },
		func() error { _, err := ListDomains(ctx, "s"); return err },
		func() error { _, err := DefaultChannelConfig(ctx, ust); return err },
		func() error { return EnableChannel(ctx, "s", ust, "c", nil) },
		func() error { return DisableChannel(ctx, "s", ust, "c") },
		func() error { _, err := ListChannels(ctx, "s", ust); return err },
		func() error { return EnableEvent(ctx, "s", ust, "c", EventRule{}) },
		func() error { return DisableEvent(ctx, "s", ust, "c", "e") },
		func() error { _, err := ListEvents(ctx, "s", ust, "c"); return err },
		func() error { _, err := ListTracepoints(ctx, ust); return err },
		func() error { return AddContext(ctx, "s", ust, "c", ContextVTID) },
	}
	for i, call := range calls {
		if err := call(); !errors.Is(err, context.Canceled) {
			t.Errorf("call %d: want context.Canceled, got %v", i, err)
		}
	}
}

func TestCancellationWhileWaiting(t *testing.T) {
	if err := lock(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := ListSessions(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline while queued, got %v", err)
	}
}

func TestDefaultChannelConfig(t *testing.T) {
	for _, domain := range []Domain{
		{Type: DomainUST, BufferType: BufferPerUID},
		{Type: DomainUST, BufferType: BufferPerPID},
		{Type: DomainKernel, BufferType: BufferGlobal},
	} {
		config, err := DefaultChannelConfig(t.Context(), domain)
		if err != nil {
			t.Fatal(err)
		}
		if err := config.validate("test defaults"); err != nil {
			t.Fatalf("invalid defaults for %+v: %+v: %v", domain, config, err)
		}
	}
}
