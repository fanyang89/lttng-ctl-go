// Package ctlstate coordinates all calls into the process-global control library.
package ctlstate

import "context"

var gate = make(chan struct{}, 1)

// Lock waits for exclusive library access, or for cancellation.
func Lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			Unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Unlock releases access acquired with Lock.
func Unlock() { <-gate }
