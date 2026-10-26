package version

import (
	"runtime/debug"
	"testing"
)

func TestStringPrefersLdflags(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })

	Version = "v0.4.2"
	if got := String(); got != "0.4.2" {
		t.Fatalf("String() = %q, want 0.4.2", got)
	}
}

func TestFromBuildInfo(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"go install @version", "v1.2.3", "1.2.3"},
		{"local checkout", "v0.1.1-0.20261026053815-cd37a8e46cdd+dirty", "0.1.1-0.20261026053815-cd37a8e46cdd+dirty"},
		{"no vcs info", "(devel)", "dev"},
		{"empty", "", "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &debug.BuildInfo{Main: debug.Module{Version: tt.version}}
			if got := fromBuildInfo(info); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
