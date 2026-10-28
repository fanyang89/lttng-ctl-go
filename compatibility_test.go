//go:build linux && cgo

package lttng_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	lttng "github.com/fanyang89/lttng-ctl-go"
	"github.com/fanyang89/lttng-ctl-go/ctl"
)

// A separate process is necessary: library selection is intentionally immutable
// after first use, and the other tests may already own native objects.
func TestRuntimeLoaderFailures(t *testing.T) {
	if mode := os.Getenv("LTTNG_CTL_GO_TEST_LOADER"); mode != "" {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := ctl.ActionNotifyCreate(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled call should not load a library: %v", err)
		}
		ctx = t.Context()
		_, err := ctl.ActionNotifyCreate(ctx)
		if mode == "missing" {
			if !errors.Is(err, ctl.ErrLibraryUnavailable) {
				t.Fatalf("missing library: %v", err)
			}
		} else {
			symbol := "lttng_action_notify_create"
			if mode == "partial" {
				symbol = "lttng_action_destroy"
			}
			missing, ok := errors.AsType[*ctl.UnsupportedError](err)
			if !errors.Is(err, ctl.ErrUnsupported) || !ok || missing.Symbol != symbol {
				t.Fatalf("missing API/destructor: %v", err)
			}
		}
		wantRoot, wantCTL := error(lttng.ErrUnsupported), ctl.ErrUnsupported
		if mode == "missing" {
			wantRoot, wantCTL = lttng.ErrLibraryUnavailable, ctl.ErrLibraryUnavailable
		}
		if alive, err := lttng.SessionDaemonAlive(ctx); alive || !errors.Is(err, wantRoot) {
			t.Fatalf("root API: alive=%v, error=%v", alive, err)
		}
		if _, err := lttng.DefaultChannelConfig(ctx, lttng.Domain{Type: lttng.DomainUST}); !errors.Is(err, wantRoot) {
			t.Fatalf("legacy channel allocation: %v", err)
		}
		if err := lttng.CreateSession(ctx, "loader-test", lttng.SessionOptions{}); !errors.Is(err, wantRoot) {
			t.Fatalf("legacy session creation: %v", err)
		}
		if _, list, err := ctl.ListSessions(ctx); list != nil || !errors.Is(err, wantCTL) {
			t.Fatalf("array result: %v, %v", list, err)
		}
		if endpoint, err := ctl.SessionDaemonNotificationEndpoint(ctx); endpoint != nil || !errors.Is(err, wantCTL) {
			t.Fatalf("global endpoint: %v, %v", endpoint, err)
		}
		if err := ctl.ChannelSetDefaultAttr(ctx, nil, nil); !errors.Is(err, wantCTL) {
			t.Fatalf("void API: %v", err)
		}
		var empty *ctl.Action
		if err := empty.Close(); err != nil {
			t.Fatalf("no-op close inherited an earlier loader failure: %v", err)
		}
		// Changing the environment cannot switch the library under live objects.
		t.Setenv("LTTNG_CTL_GO_LIBRARY", "liblttng-ctl.so.0")
		if _, err := lttng.SessionDaemonAlive(ctx); !errors.Is(err, wantRoot) {
			t.Fatalf("library selection changed after initialization: %v", err)
		}
		return
	}
	for _, mode := range []string{"missing", "empty", "partial"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			library := filepath.Join(dir, "liblttng-test.so")
			if mode != "missing" {
				source := "void unrelated(void) {}\n"
				if mode == "partial" {
					// This must never run: its destructor is absent.
					source += "#include <stdlib.h>\nvoid *lttng_action_notify_create(void) { abort(); }\n"
				}
				compile := exec.CommandContext(t.Context(), "cc", "-shared", "-fPIC", "-x", "c", "-", "-o", library)
				compile.Stdin = strings.NewReader(source)
				if output, err := compile.CombinedOutput(); err != nil {
					t.Fatalf("compile fixture: %v\n%s", err, output)
				}
			}
			child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRuntimeLoaderFailures$", "-test.v")
			child.Env = append(os.Environ(), "LTTNG_CTL_GO_TEST_LOADER="+mode, "LTTNG_CTL_GO_LIBRARY="+library)
			if output, err := child.CombinedOutput(); err != nil {
				t.Fatalf("loader subprocess: %v\n%s", err, output)
			}
		})
	}
}
