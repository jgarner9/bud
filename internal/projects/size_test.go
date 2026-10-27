package projects

import "testing"

func TestParseTree(t *testing.T) {
	out := []byte("100644 blob aaa 4000\tmain.go\n" +
		"100644 blob bbb 800\tinternal/x.go\n" +
		"100644 blob ccc 99999\tassets/logo.png\n" +
		"100644 blob ddd 50000\tgo.sum\n" +
		"160000 commit eee -\tvendor/sub\n")
	s := parseTree(out)
	if s.Files != 2 || s.LOC != 120 {
		t.Fatalf("got %+v", s)
	}
}

func TestMeasureOutsideGit(t *testing.T) {
	s, err := Measure(t.TempDir())
	if err != nil || s.Files != 0 {
		t.Fatalf("got %+v, %v", s, err)
	}
}
