package secrets

import (
	"errors"
	"strings"
	"testing"
)

// fakeKeychain records calls and serves stored values
func fakeKeychain(t *testing.T, os string) (*[]string, map[string]string) {
	t.Helper()
	var calls []string
	store := map[string]string{}
	origRun, origGOOS := run, goos
	goos = os
	run = func(stdin, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " ")+" <"+stdin)
		switch {
		case name == "security" && args[0] == "-i":
			// add-generic-password -U -s bud -a "NAME" -w "VALUE"
			f := strings.Fields(stdin)
			store[strings.Trim(f[5], `"`)] = strings.Trim(f[7], `"`)
		case name == "security" && args[0] == "find-generic-password":
			if v, ok := store[args[4]]; ok {
				return v + "\n", nil
			}
			return "", errors.New("not found")
		}
		return "", nil
	}
	t.Cleanup(func() { run, goos = origRun, origGOOS })

	return &calls, store
}

func TestEnvFirstThenKeychain(t *testing.T) {
	calls, _ := fakeKeychain(t, "darwin")
	t.Setenv("BUD_TEST_KEY", "from-env")
	if v, ok := Get("BUD_TEST_KEY"); !ok || v != "from-env" {
		t.Fatalf("env: %q %v", v, ok)
	}
	if len(*calls) != 0 {
		t.Fatalf("asked the keychain for a var that's set: %q", *calls)
	}

	t.Setenv("BUD_TEST_KEY", "")
	if Has("BUD_TEST_KEY") {
		t.Fatal("found a secret nobody stored")
	}
	if err := Set("BUD_TEST_KEY", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	if v, ok := Get("BUD_TEST_KEY"); !ok || v != "s3cr3t" {
		t.Fatalf("keychain: %q %v", v, ok)
	}
	for _, c := range *calls {
		if strings.Contains(strings.Split(c, " <")[0], "s3cr3t") {
			t.Fatalf("the value went on a command line: %q", c)
		}
	}
	if FromEnv("BUD_TEST_KEY") {
		t.Fatal("a keychain secret isn't in the env")
	}
}

func TestSetRefusesLineBreaks(t *testing.T) {
	fakeKeychain(t, "darwin")
	if err := Set("K", "a\nb"); err == nil {
		t.Fatal("stored a line break")
	}
}
