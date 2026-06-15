package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServeShutsDownOnContextCancel asserts serve returns nil promptly once the
// context is cancelled, having gracefully shut the server down.
func TestServeShutsDownOnContextCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.NewServeMux()}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv, ln) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return after context cancel")
	}
}
