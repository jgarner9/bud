package envfile

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.env")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestReadKeepsKeyValuePairsAndSkipsComments(t *testing.T) {
	p := write(t, "# a comment\nexport A=1\nB=\"two words\"\nC='3'\n\nnot a pair\n=orphan\n")
	got, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"A", "1"}, {"B", "two words"}, {"C", "3"}}
	if !slices.Equal(got, want) {
		t.Errorf("Read = %q, want %q", got, want)
	}
}

func TestValueNeedsTheKeySetAndNotEmpty(t *testing.T) {
	p := write(t, "SET=yes\nEMPTY=\n")
	if v, err := Value(p, "SET"); err != nil || v != "yes" {
		t.Errorf("Value(SET) = %q, %v", v, err)
	}
	for _, key := range []string{"EMPTY", "MISSING"} {
		if _, err := Value(p, key); err == nil {
			t.Errorf("Value(%s) accepted a key the file doesn't set", key)
		}
	}
	if _, err := Value(filepath.Join(t.TempDir(), "none"), "X"); err == nil {
		t.Error("a missing file was accepted")
	}
}

func TestWithoutAndWithReplaceTheKey(t *testing.T) {
	envv := []string{"A=1", "AB=2", "A=3"}
	if got := Without(envv, "A"); !slices.Equal(got, []string{"AB=2"}) {
		t.Errorf("Without = %q", got)
	}
	if got := With(envv, "A", "9"); !slices.Equal(got, []string{"AB=2", "A=9"}) {
		t.Errorf("With = %q", got)
	}
}
