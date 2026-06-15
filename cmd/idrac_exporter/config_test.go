package main

import (
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestShouldReload(t *testing.T) {
	cases := []struct {
		op   fsnotify.Op
		want bool
	}{
		{fsnotify.Write, true},
		{fsnotify.Remove, true},
		{fsnotify.Rename, true},
		{fsnotify.Chmod, false},
	}
	for _, tc := range cases {
		if got := shouldReload(fsnotify.Event{Op: tc.op}); got != tc.want {
			t.Fatalf("shouldReload(%v) = %v, want %v", tc.op, got, tc.want)
		}
	}
}
