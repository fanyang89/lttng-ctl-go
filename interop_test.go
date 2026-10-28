//go:build integration && linux && cgo

package lttng

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/fanyang89/lttng-ctl-go/ctl"
)

func officialCLI(t *testing.T, args ...string) []byte {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "lttng", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("official lttng %q: %v\n%s", args, err, output)
	}
	return output
}

type miEvent struct {
	Name       string   `xml:"name"`
	Enabled    bool     `xml:"enabled"`
	Filter     string   `xml:"filter_expression"`
	Exclusions []string `xml:"exclusions>exclusion"`
}

type miChannel struct {
	Name    string    `xml:"name"`
	Enabled bool      `xml:"enabled"`
	Size    uint64    `xml:"attributes>subbuffer_size"`
	Count   uint64    `xml:"attributes>subbuffer_count"`
	Events  []miEvent `xml:"events>event"`
}

type miSession struct {
	Name    string `xml:"name"`
	Enabled bool   `xml:"enabled"`
	Domains []struct {
		Channels []miChannel `xml:"channels>channel"`
	} `xml:"domains>domain"`
}

func cliSession(t *testing.T, name string) miSession {
	t.Helper()
	output := officialCLI(t, "--mi", "xml", "list", name)
	var command struct {
		Sessions []miSession `xml:"output>sessions>session"`
	}
	if err := xml.Unmarshal(output, &command); err != nil || len(command.Sessions) != 1 || command.Sessions[0].Name != name {
		t.Fatalf("CLI machine-readable session: %v\n%s", err, output)
	}
	return command.Sessions[0]
}

func interopSessionCleanup(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		if err := DestroySession(context.Background(), name); err != nil && !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("destroy interop session: %v", err)
		}
	})
}

func interopChannel(t *testing.T, name string, rule EventRule) {
	t.Helper()
	domain := Domain{Type: DomainUST, BufferType: BufferPerUID}
	config, err := DefaultChannelConfig(t.Context(), domain)
	if err != nil {
		t.Fatal(err)
	}
	config.SubbufferSize, config.SubbufferCount = 4096, 2
	if err := EnableChannel(t.Context(), name, domain, "events", &config); err != nil {
		t.Fatal(err)
	}
	if err := AddContext(t.Context(), name, domain, "events", ContextVTID); err != nil {
		t.Fatal(err)
	}
	if err := EnableEvent(t.Context(), name, domain, "events", rule); err != nil {
		t.Fatal(err)
	}
}

func TestInteropSDKToOfficialCLI(t *testing.T) {
	requireIntegration(t)
	ctx := t.Context()
	domain := Domain{Type: DomainUST, BufferType: BufferPerUID}
	name := createTestSession(t, SessionOptions{})
	rule := EventRule{Name: "interop:*", Filter: "$ctx.vtid > 0", Exclusions: []string{"interop:verbose"}}
	interopChannel(t, name, rule)
	session := cliSession(t, name)
	if len(session.Domains) != 1 || len(session.Domains[0].Channels) != 1 {
		t.Fatalf("CLI did not see SDK channel: %+v", session)
	}
	channel := session.Domains[0].Channels[0]
	if channel.Name != "events" || !channel.Enabled || channel.Size != 4096 || channel.Count != 2 || len(channel.Events) != 1 {
		t.Fatalf("CLI channel mismatch: %+v", channel)
	}
	event := channel.Events[0]
	if event.Name != rule.Name || !event.Enabled || event.Filter != rule.Filter || !slices.Equal(event.Exclusions, rule.Exclusions) {
		t.Fatalf("CLI rule mismatch: %+v", event)
	}
	officialCLI(t, "start", name)
	sessions, err := ListSessions(ctx)
	if err != nil || !slices.ContainsFunc(sessions, func(s Session) bool { return s.Name == name && s.Enabled }) {
		t.Fatalf("SDK did not see CLI start: %+v, %v", sessions, err)
	}
	if err := StopSession(ctx, name); err != nil {
		t.Fatal(err)
	}
	if cliSession(t, name).Enabled {
		t.Fatal("CLI did not see SDK stop")
	}
	officialCLI(t, "disable-event", "--userspace", "--session", name, "--channel", "events", rule.Name)
	events, err := ListEvents(ctx, name, domain, "events")
	if err != nil || len(events) != 1 || events[0].Enabled {
		t.Fatalf("SDK did not see CLI disable: %+v, %v", events, err)
	}
	if err := EnableEvent(ctx, name, domain, "events", rule); err != nil {
		t.Fatal(err)
	}
	if !cliSession(t, name).Domains[0].Channels[0].Events[0].Enabled {
		t.Fatal("CLI did not see SDK re-enable")
	}
	officialCLI(t, "destroy", name)
	if err := StartSession(ctx, name); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("SDK did not see CLI destruction: %v", err)
	}
}

func TestInteropOfficialCLIToSDK(t *testing.T) {
	requireIntegration(t)
	ctx := t.Context()
	name := fmt.Sprintf("cli-to-sdk-%d", os.Getpid())
	officialCLI(t, "create", name, "--no-output")
	interopSessionCleanup(t, name)
	officialCLI(t, "enable-channel", "--userspace", "--session", name, "--buffers-uid", "--subbuf-size=4096", "--num-subbuf=2", "events")
	officialCLI(t, "add-context", "--userspace", "--session", name, "--channel", "events", "--type=vtid")
	officialCLI(t, "enable-event", "--userspace", "--session", name, "--channel", "events", "--filter=$ctx.vtid > 0", "--exclude=interop:verbose", "--loglevel=INFO", "interop:*")
	domain := Domain{Type: DomainUST, BufferType: BufferPerUID}
	channels, err := ListChannels(ctx, name, domain)
	if err != nil || len(channels) != 1 || channels[0].Config.SubbufferSize != 4096 || channels[0].Config.SubbufferCount != 2 {
		t.Fatalf("SDK channel mismatch: %+v, %v", channels, err)
	}
	events, err := ListEvents(ctx, name, domain, "events")
	if err != nil || len(events) != 1 || events[0].Filter != "$ctx.vtid > 0" || events[0].LogLevel != 6 || events[0].LogLevelType != LogLevelRange || !slices.Equal(events[0].Exclusions, []string{"interop:verbose"}) {
		t.Fatalf("SDK rule mismatch: %+v, %v", events, err)
	}
	if err := StartSession(ctx, name); err != nil {
		t.Fatal(err)
	}
	if !cliSession(t, name).Enabled {
		t.Fatal("CLI did not see SDK start")
	}
	officialCLI(t, "stop", name)
	if err := DisableChannel(ctx, name, domain, "events"); err != nil {
		t.Fatal(err)
	}
	if cliSession(t, name).Domains[0].Channels[0].Enabled {
		t.Fatal("CLI did not see SDK channel disable")
	}
}

func TestInteropSaveLoad(t *testing.T) {
	requireIntegration(t)
	ctx := t.Context()
	name := createTestSession(t, SessionOptions{OutputPath: filepath.Join(t.TempDir(), "trace")})
	interopChannel(t, name, EventRule{Name: "interop:*", Filter: "$ctx.vtid > 0"})
	cliDir := t.TempDir()
	officialCLI(t, "save", name, "--output-path", cliDir)
	if err := DestroySession(ctx, name); err != nil {
		t.Fatal(err)
	}
	load, err := ctl.LoadSessionAttrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer load.Close()
	cliFile := filepath.Join(cliDir, name+".lttng")
	if _, err := ctl.LoadSessionAttrSetInputURL(ctx, load, &cliFile); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.LoadSessionAttrSetSessionName(ctx, load, &name); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.LoadSession(ctx, load); err != nil {
		t.Fatal(err)
	}
	loaded, err := ListSessions(ctx)
	if err != nil || !slices.ContainsFunc(loaded, func(s Session) bool { return s.Name == name }) {
		config, _ := os.ReadFile(filepath.Join(cliDir, name+".lttng"))
		t.Fatalf("SDK load did not restore CLI session: %+v, %v\n%s", loaded, err, config)
	}
	sdkDir := t.TempDir()
	save, err := ctl.SaveSessionAttrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer save.Close()
	if _, err := ctl.SaveSessionAttrSetOutputURL(ctx, save, &sdkDir); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.SaveSessionAttrSetSessionName(ctx, save, &name); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.SaveSession(ctx, save); err != nil {
		t.Fatal(err)
	}
	if err := DestroySession(ctx, name); err != nil {
		t.Fatal(err)
	}
	officialCLI(t, "load", name, "--input-path", sdkDir)
	if session := cliSession(t, name); len(session.Domains) != 1 || session.Domains[0].Channels[0].Events[0].Filter != "$ctx.vtid > 0" {
		t.Fatalf("cross-language save/load mismatch: %+v", session)
	}
}

func TestInteropUpstreamEventGenerator(t *testing.T) {
	requireIntegration(t)
	generator := os.Getenv("LTTNG_UPSTREAM_EVENT_GENERATOR")
	if generator == "" {
		t.Skip("run scripts/test-upstream.sh sdk to use the unmodified upstream UST fixture")
	}
	for _, snapshot := range []bool{false, true} {
		for _, sdkCreates := range []bool{false, true} {
			t.Run(fmt.Sprintf("snapshot=%v/sdkCreates=%v", snapshot, sdkCreates), func(t *testing.T) {
				ctx := t.Context()
				name := fmt.Sprintf("upstream-interop-%d-%v-%v", os.Getpid(), snapshot, sdkCreates)
				trace := filepath.Join(t.TempDir(), "trace")
				if sdkCreates {
					if snapshot {
						desc, err := ctl.SessionDescriptorSnapshotLocalCreate(ctx, &name, &trace)
						if err != nil {
							t.Fatal(err)
						}
						defer desc.Close()
						if _, err := ctl.CreateSessionExt(ctx, desc); err != nil {
							t.Fatal(err)
						}
					} else if err := CreateSession(ctx, name, SessionOptions{OutputPath: trace}); err != nil {
						t.Fatal(err)
					}
				} else {
					args := []string{"create", name, "--output", trace}
					if snapshot {
						args = append(args, "--snapshot")
					}
					officialCLI(t, args...)
				}
				interopSessionCleanup(t, name)
				interopChannel(t, name, EventRule{Name: "tp:tptest", Filter: "intfield >= 5"})
				if sdkCreates {
					officialCLI(t, "start", name)
				} else if err := StartSession(ctx, name); err != nil {
					t.Fatal(err)
				}
				command := exec.CommandContext(ctx, generator, "-i", "10", "-w", "0")
				command.Env = append(os.Environ(), "LTTNG_UST_REGISTER_TIMEOUT=-1")
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("upstream generator: %v\n%s", err, output)
				}
				if snapshot {
					if sdkCreates {
						officialCLI(t, "snapshot", "record", "--session", name)
					} else if _, err := ctl.SnapshotRecord(ctx, &name, nil, 1); err != nil {
						t.Fatal(err)
					}
				}
				if sdkCreates {
					if err := StopSession(ctx, name); err != nil {
						t.Fatal(err)
					}
				} else {
					officialCLI(t, "stop", name)
				}
				if err := DestroySession(ctx, name); err != nil {
					t.Fatal(err)
				}
				const check = `import bt2, sys
events = [m.event for m in bt2.TraceCollectionMessageIterator(sys.argv[1]) if isinstance(m, bt2._EventMessageConst)]
assert len(events) == 5, len(events)
assert sorted(int(e["intfield"]) for e in events) == list(range(5, 10))
assert all(e.name == "tp:tptest" and int(e.common_context_field["vtid"]) > 0 for e in events)
print("verified 5 filtered upstream events and VTID context")
`
				if output, err := exec.CommandContext(ctx, "/usr/bin/python3", "-B", "-c", check, trace).CombinedOutput(); err != nil {
					t.Fatalf("Babeltrace interoperability: %v\n%s", err, output)
				}
			})
		}
	}
}

func TestInteropNoOutputLoadMatchesNative(t *testing.T) {
	requireIntegration(t)
	ctx := t.Context()
	name := createTestSession(t, SessionOptions{})
	interopChannel(t, name, EventRule{Name: "interop:*"})
	configDir := t.TempDir()
	officialCLI(t, "save", name, "--output-path", configDir)
	if err := DestroySession(ctx, name); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, name+".lttng")
	flags, err := exec.CommandContext(ctx, "pkg-config", "--cflags", "--libs", "lttng-ctl").Output()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "load-session")
	args := append([]string{"testdata/load-session.c", "-o", binary}, strings.Fields(string(flags))...)
	if output, err := exec.CommandContext(ctx, "cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile native reference: %v\n%s", err, output)
	}
	load, err := ctl.LoadSessionAttrCreate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer load.Close()
	if _, err := ctl.LoadSessionAttrSetSessionName(ctx, load, &name); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{configFile, configDir} {
		output, err := exec.CommandContext(ctx, binary, path, name).Output()
		if err != nil {
			t.Fatalf("native load reference: %v", err)
		}
		code, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err != nil {
			t.Fatal(err)
		}
		// Remove a successfully restored session before testing the Go operation.
		sessions, err := ListSessions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		nativeRestored := slices.ContainsFunc(sessions, func(s Session) bool { return s.Name == name })
		if nativeRestored {
			if err := DestroySession(ctx, name); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ctl.LoadSessionAttrSetInputURL(ctx, load, &path); err != nil {
			t.Fatal(err)
		}
		result, goErr := ctl.LoadSession(ctx, load)
		if code < 0 {
			native, ok := errors.AsType[*ctl.StatusError](goErr)
			if !ok || native.Code != code {
				t.Fatalf("native returned %d, Go returned %d, %v", code, result, goErr)
			}
		} else if goErr != nil || result != code {
			t.Fatalf("native returned %d, Go returned %d, %v", code, result, goErr)
		}
		sessions, err = ListSessions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		goRestored := slices.ContainsFunc(sessions, func(s Session) bool { return s.Name == name })
		if goRestored != nativeRestored {
			t.Fatalf("native restored=%v, Go restored=%v", nativeRestored, goRestored)
		}
		t.Logf("load reference: directory=%v native_code=%d restored=%v (Go matches)", path == configDir, code, nativeRestored)
		if code == 0 && !nativeRestored {
			t.Logf("UPSTREAM BEHAVIOR: native and Go both return success without restoring the no-output session from directory %s", path)
		}
		if goRestored {
			if err := DestroySession(ctx, name); err != nil {
				t.Fatal(err)
			}
		}
	}
}
