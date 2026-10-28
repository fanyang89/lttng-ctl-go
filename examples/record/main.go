//go:build linux && cgo

// Record illustrates the SDK lifecycle. Run an instrumented application while
// this program is recording, then interrupt this program to drain and finish.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	lttng "github.com/fanyang89/lttng-ctl-go"
)

func main() {
	name := flag.String("session", "lttng-ctl-go-example", "session name")
	output := flag.String("output", "./traces", "trace output directory")
	pattern := flag.String("event", "*", "UST event name pattern")
	filter := flag.String("filter", "", "LTTng event filter expression")
	flag.Parse()
	path, err := filepath.Abs(*output)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := record(ctx, *name, path, *pattern, *filter); err != nil {
		log.Fatal(err)
	}
}

func record(ctx context.Context, name, path, pattern, filter string) (err error) {
	domain := lttng.Domain{Type: lttng.DomainUST, BufferType: lttng.BufferPerUID}
	if err := lttng.CreateSession(ctx, name, lttng.SessionOptions{OutputPath: path}); err != nil {
		return err
	}
	defer func() {
		// Cleanup must not use the canceled recording context. Native draining
		// cannot be interrupted; a timeout context would not bound that C call.
		err = errors.Join(err, lttng.DestroySession(context.Background(), name))
	}()
	if err := lttng.EnableChannel(ctx, name, domain, "events", nil); err != nil {
		return err
	}
	if err := lttng.AddContext(ctx, name, domain, "events", lttng.ContextVTID); err != nil {
		return err
	}
	if err := lttng.EnableEvent(ctx, name, domain, "events", lttng.EventRule{Name: pattern, Filter: filter}); err != nil {
		return err
	}
	if err := lttng.StartSession(ctx, name); err != nil {
		return err
	}
	fmt.Printf("Recording %s to %s; press Ctrl+C to stop.\n", name, path)
	<-ctx.Done()
	return lttng.StopSession(context.Background(), name)
}
