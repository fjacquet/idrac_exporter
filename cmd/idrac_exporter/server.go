package main

import (
	"context"
	"net"
	"net/http"
	"time"
)

// serve runs srv on ln until it errors or ctx is cancelled. On cancellation it
// gracefully shuts the server down with a bounded 10s timeout and returns the
// result of Shutdown (nil on a clean drain, otherwise the Shutdown error).
func serve(ctx context.Context, srv *http.Server, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shCtx)
	}
}
