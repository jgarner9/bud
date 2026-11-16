package cli

import (
	"context"
	"net/http"
	"testing"
)

// the command serves on the loopback address it's given and refuses any
// other, or an argument
func TestMCPOpsListensOnLoopbackOnly(t *testing.T) {
	var served string
	orig := serveOps
	serveOps = func(_ context.Context, listen string, _ http.Handler) error { served = listen; return nil }
	t.Cleanup(func() { serveOps = orig })

	if err := runMCPOps(&Globals{}, nil, "127.0.0.1:8792"); err != nil {
		t.Fatal(err)
	}
	if served != "127.0.0.1:8792" {
		t.Errorf("served %q", served)
	}
	for _, listen := range []string{"0.0.0.0:8792", "100.64.0.1:8792", "localhost:8792", "127.0.0.1"} {
		if err := runMCPOps(&Globals{}, nil, listen); err == nil {
			t.Errorf("listen %q was accepted", listen)
		}
	}
	if err := runMCPOps(&Globals{}, []string{"x"}, "127.0.0.1:8792"); err == nil {
		t.Error("an argument was accepted")
	}
}
