package locate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTools answers by tool name and records every call.
type fakeTools struct {
	out   map[string]Output
	err   map[string]error
	calls [][]string
}

func (f *fakeTools) run(name string, args ...string) (Output, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if err := f.err[name]; err != nil {
		return Output{}, err
	}
	return f.out[name], nil
}

func (f *fakeTools) called(name string) []string {
	for _, c := range f.calls {
		if c[0] == name {
			return c
		}
	}
	return nil
}

// rgOut formats hits the way ripgrep prints them with --null.
func rgOut(lines ...string) Output {
	var b strings.Builder
	for _, l := range lines { // "path:line:text"
		p := strings.SplitN(l, ":", 3)
		fmt.Fprintf(&b, "%s\x00%s:%s\n", p[0], p[1], p[2])
	}
	return Output{Stdout: []byte(b.String())}
}

func rgOpts(f *fakeTools, q string) Options {
	return Options{Query: q, Tools: Tools{RG: "rg"}, Run: f.run}
}

func TestDefinitionsComeBeforeUsesAndTestsLast(t *testing.T) {
	f := &fakeTools{out: map[string]Output{"rg": rgOut(
		"a/use.go:3:\tx := Parse(in)",
		"a/parse_test.go:5:\tParse(\"x\")",
		"a/parse.go:10:func Parse(s string) int {",
		"a/parse_test.go:2:func Parse(t *testing.T) {",
		"b/main.go:7:var Parse = other",
	)}}
	res, err := Run(rgOpts(f, "Parse"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range res.Hits {
		got = append(got, fmt.Sprintf("%s:%d:%s", h.File, h.Line, h.Kind))
	}
	// Defs rank first; a file's hits are then shown together, lines ascending.
	want := "a/parse.go:10:def b/main.go:7:def a/parse_test.go:2:def a/parse_test.go:5:use a/use.go:3:use"
	if strings.Join(got, " ") != want {
		t.Errorf("got  %v\nwant %s", got, want)
	}
	if res.Defs != 3 || res.Total != 5 || res.Files != 4 {
		t.Errorf("counts: %+v", res)
	}
}

func TestTheQueryIsOneArgumentAfterDoubleDash(t *testing.T) {
	f := &fakeTools{out: map[string]Output{"rg": {}}}
	o := rgOpts(f, "--files; rm -rf $HOME")
	o.Paths = []string{"-weird", "src"}
	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}
	args := f.called("rg")
	tail := strings.Join(args[len(args)-4:], "|")
	if tail != "--|--files; rm -rf $HOME|-weird|src" {
		t.Errorf("rg args end %q", tail)
	}
	if strings.Contains(strings.Join(args, " "), "--word-regexp") {
		t.Error("a phrase must not be searched as a whole word")
	}
}

func TestAstGrepFindsDefinitionsTheRegexesMiss(t *testing.T) {
	f := &fakeTools{out: map[string]Output{
		"rg": rgOut("x.go:4:\tfoo,", "x.go:9:\treturn foo"),
		// ast-grep lines are 0-based: line 3 is line 4 of the file.
		"ast-grep": {Stdout: []byte(`{"file":"./x.go","range":{"start":{"line":3,"column":1}}}` + "\n")},
	}}
	o := rgOpts(f, "foo")
	o.Tools.AstGrep = "ast-grep"
	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hits[0].Kind != KindDef || res.Hits[0].Line != 4 || res.Hits[1].Kind != KindUse {
		t.Errorf("hits %+v", res.Hits)
	}
	if strings.Join(res.Tools, ",") != "rg,ast-grep" {
		t.Errorf("tools %v", res.Tools)
	}
	args := f.called("ast-grep")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, `"kind":"type_spec"`) || !strings.Contains(joined, `foo$`) || args[len(args)-2] != "--" || args[len(args)-1] != "." {
		t.Errorf("ast-grep args %q", args)
	}
	if n := countCalls(f, "ast-grep"); n != 1 {
		t.Errorf("ast-grep ran %d times; once per language with hits", n)
	}
}

func countCalls(f *fakeTools, name string) int {
	n := 0
	for _, c := range f.calls {
		if c[0] == name {
			n++
		}
	}
	return n
}

func TestAstGrepFailureKeepsTheRegexesAndSaysSo(t *testing.T) {
	f := &fakeTools{out: map[string]Output{
		"rg":       rgOut("x.py:1:def foo():", "x.py:5:foo()"),
		"ast-grep": {Exit: 2, Stderr: "error: unknown flag --inline-rules\nusage"},
	}}
	o := rgOpts(f, "foo")
	o.Tools.AstGrep = "ast-grep"
	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Defs != 1 || res.Hits[0].Kind != KindDef {
		t.Errorf("regex fallback lost: %+v", res)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "unknown flag --inline-rules") || strings.Join(res.Tools, ",") != "rg" {
		t.Errorf("notes %v tools %v", res.Notes, res.Tools)
	}
}

func TestRegexFallbackRecognisesDefinitionsInEveryLanguage(t *testing.T) {
	cases := []struct{ file, text string }{
		{"a.go", "func (s *Server) Foo(x int) error {"},
		{"a.go", "type Foo struct {"},
		{"a.go", "const Foo = 3"},
		{"a.lua", "function M.Foo(a)"},
		{"a.lua", "local function Foo()"},
		{"a.lua", "M.Foo = function(a)"},
		{"a.py", "    async def Foo(self):"},
		{"a.py", "class Foo(Base):"},
		{"a.ts", "export default async function Foo() {"},
		{"a.ts", "export interface Foo {"},
		{"a.ts", "  private static Foo(x: number): void {"},
		{"a.js", "const Foo = () => {"},
		{"a.rs", "    pub(crate) async fn Foo<T>(x: T) {"},
		{"a.rs", "pub struct Foo;"},
		{"a.sh", "Foo() {"},
		{"a.sh", "function Foo {"},
		{"a.sh", "export Foo=1"},
	}
	for _, c := range cases {
		f := &fakeTools{out: map[string]Output{"rg": rgOut(c.file + ":1:" + c.text)}}
		res, err := Run(rgOpts(f, "Foo"))
		if err != nil || res.Hits[0].Kind != KindDef {
			t.Errorf("%s %q should be a definition (err %v)", c.file, c.text, err)
		}
	}
	for _, c := range []struct{ file, text string }{
		{"a.go", "x := Foo(1)"}, {"a.go", "// Foo does a thing"}, {"a.py", "    return Foo()"},
		{"a.ts", "if (Foo(x)) {"}, {"a.rs", "let x = Foo::new();"}, {"notes.md", "func Foo() {"},
	} {
		f := &fakeTools{out: map[string]Output{"rg": rgOut(c.file + ":1:" + c.text)}}
		res, _ := Run(rgOpts(f, "Foo"))
		if res.Hits[0].Kind != KindUse {
			t.Errorf("%s %q should be a use", c.file, c.text)
		}
	}
}

func TestMaxCapsHitsAndCountsWhatWasCut(t *testing.T) {
	var lines []string
	lines = append(lines, "d.go:1:func Foo() {")
	for i := 1; i <= 9; i++ {
		lines = append(lines, fmt.Sprintf("u.go:%d:Foo()", i))
	}
	f := &fakeTools{out: map[string]Output{"rg": rgOut(lines...)}}
	o := rgOpts(f, "Foo")
	o.Max = 3
	res, _ := Run(o)
	if len(res.Hits) != 3 || res.Total != 10 || res.Cut != 7 || res.CutDefs != 0 || res.Hits[0].Kind != KindDef {
		t.Errorf("%+v", res)
	}
	o.Max = 0
	if res, _ = Run(o); len(res.Hits) != 10 || res.Cut != 0 {
		t.Errorf("--max 0 should return everything: %+v", res)
	}
}

func TestKindFiltersAndPhrasesHaveNoDefinitions(t *testing.T) {
	f := &fakeTools{out: map[string]Output{"rg": rgOut("a.go:1:func Foo() {", "a.go:2:Foo()")}}
	o := rgOpts(f, "Foo")
	o.Kind = KindDef
	if res, _ := Run(o); len(res.Hits) != 1 || res.Total != 1 || res.Hits[0].Line != 1 {
		t.Errorf("def: %+v", res)
	}
	o.Kind = KindUse
	if res, _ := Run(o); len(res.Hits) != 1 || res.Hits[0].Line != 2 {
		t.Errorf("use: %+v", res)
	}
	f.out["rg"] = rgOut("a.go:1:the quick fox")
	res, _ := Run(rgOpts(f, "quick fox"))
	if res.Symbol || res.Hits[0].Kind != KindMatch || res.Defs != 0 {
		t.Errorf("phrase: %+v", res)
	}
}

func TestRepomapRanksTheFilesForAPhrase(t *testing.T) {
	f := &fakeTools{out: map[string]Output{
		"rg":      rgOut("zz/low.go:1:retry the call", "aa/other.go:1:retry the call", "core/high.go:9:retry the call"),
		"repomap": {Stdout: []byte("core/high.go\n  func Run\nzz/low.go\n  func X\n")},
	}}
	o := rgOpts(f, "retry the call")
	o.Tools.Repomap = "repomap"
	res, _ := Run(o)
	var files []string
	for _, h := range res.Hits {
		files = append(files, h.File)
	}
	if strings.Join(files, ",") != "core/high.go,zz/low.go,aa/other.go" || strings.Join(res.Tools, ",") != "rg,repomap" {
		t.Errorf("files %v tools %v", files, res.Tools)
	}
	// A symbol never asks the map.
	f.calls = nil
	f.out["rg"] = rgOut("a.go:1:Foo()", "b.go:1:Foo()")
	o.Query = "Foo"
	Run(o)
	if f.called("repomap") != nil {
		t.Error("repomap should only rank phrases")
	}
}

func TestGrepIsTheFallbackAndSaysSo(t *testing.T) {
	f := &fakeTools{out: map[string]Output{"grep": {Stdout: []byte("./a/b.go:3:func Foo() {\n")}}}
	res, err := Run(Options{Query: "Foo", Tools: Tools{Grep: "grep"}, Run: f.run})
	if err != nil {
		t.Fatal(err)
	}
	if res.Hits[0].File != "a/b.go" || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "ripgrep not found") || res.Tools[0] != "grep" {
		t.Errorf("%+v", res)
	}
	args := f.called("grep")
	if strings.Join(args[len(args)-3:], "|") != "--|Foo|." {
		t.Errorf("grep args %q", args)
	}
	if _, err := Run(Options{Query: "Foo", Run: f.run}); err == nil || !strings.Contains(err.Error(), "install ripgrep") {
		t.Errorf("no tools: %v", err)
	}
}

func TestToolFailuresSayWhatToDoNext(t *testing.T) {
	f := &fakeTools{out: map[string]Output{"rg": {Exit: 2, Stderr: "nope: No such file or directory (os error 2)\n"}}}
	if _, err := Run(rgOpts(f, "Foo")); err == nil || !strings.Contains(err.Error(), "No such file") || !strings.Contains(err.Error(), "path") {
		t.Errorf("exit 2: %v", err)
	}
	f = &fakeTools{err: map[string]error{"rg": errors.New("boom")}}
	if _, err := Run(rgOpts(f, "Foo")); err == nil || !strings.Contains(err.Error(), "run rg: boom") {
		t.Errorf("start failure: %v", err)
	}
	f = &fakeTools{out: map[string]Output{"rg": func() Output { o := rgOut("a.go:1:Foo"); o.Exit = 2; return o }()}}
	if res, err := Run(rgOpts(f, "Foo")); err != nil || len(res.Hits) != 1 || len(res.Notes) != 1 {
		t.Errorf("partial results should be kept with a note: %+v %v", res, err)
	}
}

func TestPathsWithColonsAndLongLinesParse(t *testing.T) {
	f := &fakeTools{out: map[string]Output{"rg": {Stdout: []byte("odd:dir/a.go\x003:x: " + strings.Repeat("é", 300) + "\n")}}}
	res, _ := Run(rgOpts(f, "x"))
	h := res.Hits[0]
	if h.File != "odd:dir/a.go" || h.Line != 3 || !strings.HasPrefix(h.Text, "x: éé") || len([]rune(h.Text)) != maxText+1 || !strings.HasSuffix(h.Text, "…") {
		t.Errorf("%+v", h)
	}
}

func TestFindAstGrepRejectsTheSystemSg(t *testing.T) {
	look := func(n string) (string, error) { return "/usr/bin/" + n, nil }
	shadow := func(name string, args ...string) (Output, error) {
		if strings.HasSuffix(name, "ast-grep") {
			return Output{}, errors.New("not found")
		}
		return Output{Exit: 1, Stderr: "Usage: sg group [[-c] command]"}, nil
	}
	if got := FindAstGrep(look, shadow); got != "" {
		t.Errorf("shadow-utils sg accepted as %q", got)
	}
	real := func(name string, args ...string) (Output, error) {
		if strings.HasSuffix(name, "/sg") {
			return Output{Stdout: []byte("ast-grep 0.38.0\n")}, nil
		}
		return Output{}, errors.New("missing")
	}
	if got := FindAstGrep(look, real); got != "/usr/bin/sg" {
		t.Errorf("got %q", got)
	}
}

func TestParseLangAcceptsAliases(t *testing.T) {
	for in, want := range map[string]string{"go": "go", "PY": "python", "ts": "typescript", "rs": "rust", "bash": "shell"} {
		if got, ok := ParseLang(in); !ok || got != want {
			t.Errorf("%s -> %q %v", in, got, ok)
		}
	}
	if _, ok := ParseLang("cobol"); ok {
		t.Error("cobol is not supported")
	}
}

// --- against the real tools; each skips when its tool is not installed ---

func writeTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"pkg/parse.go":       "package pkg\n\n// Parse reads input.\nfunc Parse(s string) int {\n\treturn len(s)\n}\n",
		"pkg/use.go":         "package pkg\n\nfunc run() int {\n\treturn Parse(\"a\")\n}\n",
		"pkg/parse_test.go":  "package pkg\n\nfunc TestX() { _ = Parse(\"b\") }\n",
		"lib/parse.lua":      "local M = {}\nfunction M.Parse(s)\n  return s\nend\nreturn M\n",
		"app/parse.py":       "def Parse(s):\n    return s\n\nx = Parse(1)\n",
		"web/parse.ts":       "export function Parse(s: string) {\n  return s\n}\n",
		"src/lib.rs":         "pub fn Parse(s: &str) -> usize {\n    s.len()\n}\n",
		"bin/run.sh":         "Parse() {\n  echo hi\n}\nParse\n",
		"ignored/skipped.go": "package ignored\n\nfunc Parse() {}\n",
		"notes.txt":          "we should Parse more carefully\n",
		".gitignore":         "ignored/\n",
	}
	for p, body := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func realRG(t *testing.T) string {
	p, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("rg is not installed")
	}
	return p
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
}

func TestRipgrepFindsDefinitionsFirstAndHonoursGitignore(t *testing.T) {
	rg := realRG(t)
	dir := writeTree(t)
	gitInit(t, dir)
	t.Chdir(dir)
	res, err := Run(Options{Query: "Parse", Tools: Tools{RG: rg}})
	if err != nil {
		t.Fatal(err)
	}
	defs := map[string]bool{}
	for _, h := range res.Hits {
		if strings.HasPrefix(h.File, "ignored/") {
			t.Errorf("ignored file listed: %+v", h)
		}
		if h.Kind == KindDef {
			defs[h.File] = true
		}
	}
	for _, f := range []string{"pkg/parse.go", "lib/parse.lua", "app/parse.py", "web/parse.ts", "src/lib.rs", "bin/run.sh"} {
		if !defs[f] {
			t.Errorf("%s should hold a definition; defs %v", f, defs)
		}
	}
	if res.Hits[0].Kind != KindDef || res.Hits[0].File == "pkg/parse_test.go" {
		t.Errorf("first hit %+v", res.Hits[0])
	}
	if res.Hits[0].Text == "" || res.Hits[0].Line < 1 {
		t.Errorf("hit %+v", res.Hits[0])
	}

	res, _ = Run(Options{Query: "Parse", Lang: "python", Tools: Tools{RG: rg}})
	for _, h := range res.Hits {
		if h.File != "app/parse.py" {
			t.Errorf("--lang python returned %s", h.File)
		}
	}
	res, _ = Run(Options{Query: "Parse", Paths: []string{"lib"}, Tools: Tools{RG: rg}})
	if len(res.Hits) != 1 || res.Hits[0].File != "lib/parse.lua" || res.Hits[0].Line != 2 {
		t.Errorf("path scope: %+v", res.Hits)
	}
	res, _ = Run(Options{Query: "more carefully", Tools: Tools{RG: rg}})
	if len(res.Hits) != 1 || res.Hits[0].File != "notes.txt" || res.Hits[0].Kind != KindMatch {
		t.Errorf("phrase: %+v", res.Hits)
	}
}

func TestGrepFallbackWorksForReal(t *testing.T) {
	grep, err := exec.LookPath("grep")
	if err != nil {
		t.Skip("grep is not installed")
	}
	dir := writeTree(t)
	t.Chdir(dir)
	res, err := Run(Options{Query: "Parse", Lang: "go", Tools: Tools{Grep: grep}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) < 3 || res.Hits[0].Kind != KindDef || res.Notes == nil || len(res.Notes) == 0 {
		t.Errorf("%+v", res)
	}
	for _, h := range res.Hits {
		if !strings.HasSuffix(h.File, ".go") || strings.HasPrefix(h.File, "./") {
			t.Errorf("hit %+v", h)
		}
	}

	file := filepath.Join(dir, "pkg", "parse.go")
	res, err = Run(Options{Query: "Parse", Paths: []string{file}, Lang: "go", Tools: Tools{Grep: grep}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 2 || res.Hits[0].File != file {
		t.Errorf("single-file scope: %+v", res.Hits)
	}
}

func TestAstGrepFindsDeclarationsInEveryLanguage(t *testing.T) {
	rg := realRG(t)
	ast := FindAstGrep(exec.LookPath, ExecRunner)
	if ast == "" {
		t.Skip("ast-grep is not installed")
	}
	dir := writeTree(t)
	gitInit(t, dir)
	t.Chdir(dir)
	res, err := Run(Options{Query: "Parse", Kind: KindDef, Tools: Tools{RG: rg, AstGrep: ast}})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range res.Notes {
		t.Errorf("note: %s", n)
	}
	if !strings.Contains(strings.Join(res.Tools, ","), "ast-grep") {
		t.Errorf("ast-grep did not run: %v", res.Tools)
	}
	got := map[string]bool{}
	for _, h := range res.Hits {
		got[key(h.File, h.Line)] = true
	}
	for _, k := range []string{"pkg/parse.go:4", "lib/parse.lua:2", "app/parse.py:1", "web/parse.ts:1", "src/lib.rs:1", "bin/run.sh:1"} {
		if !got[k] {
			t.Errorf("missing definition %s in %v", k, got)
		}
	}
}

func TestGrepUsesPortableFilenameOutputAndUnreadableOutputFailsLoudly(t *testing.T) {
	f := &fakeTools{out: map[string]Output{"grep": {Stdout: []byte("a.go:1:func Foo() {\n")}}}
	if _, err := Run(Options{Query: "Foo", Tools: Tools{Grep: "grep"}, Run: f.run}); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(f.called("grep"), " ")
	if !strings.Contains(args, "-rnIFH") || strings.Contains(args, "--exclude-dir") ||
		strings.Contains(args, "--include=") || strings.Contains(args, "--null") {
		t.Errorf("grep args must use portable filename output and no GNU-only options: %q", args)
	}
	f.out["grep"] = Output{Stdout: []byte("not grep output\n")}
	_, err := Run(Options{Query: "Foo", Tools: Tools{Grep: "grep"}, Run: f.run})
	if err == nil || !strings.Contains(err.Error(), "could not read") {
		t.Errorf("unparsable output must be an error, not 'no matches': %v", err)
	}
}

func TestTruncatedAstGrepOutputFallsBackToRegexes(t *testing.T) {
	f := &fakeTools{out: map[string]Output{
		"rg": rgOut("s.go:1:func Foo() {", "s.go:3:) Foo(x int) {"),
		"ast-grep": {
			Stdout:    []byte(`{"file":"s.go","range":{"start":{"line":2}}}` + "\n"),
			Truncated: true,
		},
	}}
	o := rgOpts(f, "Foo")
	o.Tools.AstGrep = "ast-grep"
	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Hits[0].Kind != KindDef || res.Hits[1].Kind != KindUse {
		t.Errorf("partial ast-grep output was used: %+v", res.Hits)
	}
	if strings.Join(res.Tools, ",") != "rg" || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "output truncated") {
		t.Errorf("truncated scan not reported: tools %v notes %v", res.Tools, res.Notes)
	}
}

func TestRipgrepIgnoresUserConfig(t *testing.T) {
	f := &fakeTools{out: map[string]Output{"rg": {}}}
	Run(rgOpts(f, "Foo"))
	if a := f.called("rg"); a[1] != "--no-config" {
		t.Errorf("rg args %q", a)
	}
}

func TestIndentedAssignmentsAreNotDefinitions(t *testing.T) {
	f := &fakeTools{out: map[string]Output{"rg": rgOut(
		"a.py:2:    timeout=5,", "a.py:9:timeout = 5", "b.sh:3:    timeout=5")}}
	res, _ := Run(rgOpts(f, "timeout"))
	got := map[string]string{}
	for _, h := range res.Hits {
		got[key(h.File, h.Line)] = h.Kind
	}
	if got["a.py:9"] != KindDef || got["a.py:2"] != KindUse {
		t.Errorf("python: %v", got)
	}
	if got["b.sh:3"] != KindDef { // shell allows indented assignments
		t.Errorf("shell: %v", got)
	}
	if h := res.Hits[len(res.Hits)-1]; strings.HasPrefix(h.Text, " ") {
		t.Errorf("display text must be trimmed: %q", h.Text)
	}
}

func TestAstGrepNameLineCatchesWrappedAndDecoratedDeclarations(t *testing.T) {
	f := &fakeTools{out: map[string]Output{
		"rg": rgOut("s.go:3:) Foo(x int) {", "s.go:9:\tFoo(1)", "d.ts:2:class Foo {"),
		// the name node is what ast-grep reports; 0-based lines 2 and 1
		"ast-grep": {Stdout: []byte(`{"file":"s.go","range":{"start":{"line":2}}}` + "\n" + `{"file":"d.ts","range":{"start":{"line":1}}}` + "\n")},
	}}
	o := rgOpts(f, "Foo")
	o.Tools.AstGrep = "ast-grep"
	res, _ := Run(o)
	got := map[string]string{}
	for _, h := range res.Hits {
		got[key(h.File, h.Line)] = h.Kind
	}
	if got["s.go:3"] != KindDef || got["d.ts:2"] != KindDef || got["s.go:9"] != KindUse {
		t.Errorf("%v", got)
	}
	rule := strings.Join(f.called("ast-grep"), " ")
	if !strings.Contains(rule, `"inside":{"any":`) || !strings.Contains(rule, `"field":"name"`) {
		t.Errorf("the rule should match the name node: %s", rule)
	}
}

func TestATruncatedLastLineIsDropped(t *testing.T) {
	out := rgOut("a.go:1:Foo()", "a.go:2:Foo()")
	out.Stdout = append(out.Stdout, []byte("a.go\x003:Fo")...) // cut mid-line
	out.Truncated = true
	f := &fakeTools{out: map[string]Output{"rg": out}}
	res, _ := Run(rgOpts(f, "Foo"))
	if len(res.Hits) != 2 || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "stopped reading") {
		t.Errorf("%+v", res)
	}
}

func TestControlFlowKeywordsAreNotMethodDefinitions(t *testing.T) {
	for _, q := range []string{"if", "catch", "for", "switch"} {
		f := &fakeTools{out: map[string]Output{"rg": rgOut("a.ts:1:  " + q + " (x) {")}}
		if res, _ := Run(rgOpts(f, q)); res.Hits[0].Kind != KindUse {
			t.Errorf("%s (x) { is control flow", q)
		}
	}
	f := &fakeTools{out: map[string]Output{"rg": rgOut("a.ts:1:  run(x) {")}}
	if res, _ := Run(rgOpts(f, "run")); res.Hits[0].Kind != KindDef {
		t.Error("a real method must stay a definition")
	}
}

func TestRepomapMatchesWholePathsAndGetsTheSearchedRoot(t *testing.T) {
	f := &fakeTools{out: map[string]Output{
		"rg":      rgOut("src/a.go:1:retry now", "src/data.go:1:retry now", "src/b.go:1:retry now"),
		"repomap": {Stdout: []byte("data.go: 12 refs\nb.go (3)\n")},
	}}
	o := rgOpts(f, "retry now")
	o.Tools.Repomap = "repomap"
	o.Paths = []string{"src"}
	res, _ := Run(o)
	var files []string
	for _, h := range res.Hits {
		files = append(files, h.File)
	}
	// a.go is a substring of data.go but not on the map, so it ranks last.
	if strings.Join(files, ",") != "src/data.go,src/b.go,src/a.go" {
		t.Errorf("files %v", files)
	}
	if a := f.called("repomap"); a[2] != "src" {
		t.Errorf("repomap args %q", a)
	}
}

func TestAstGrepIsOnlyLookedForWhenThereAreSymbolHitsToClassify(t *testing.T) {
	f := &fakeTools{out: map[string]Output{"rg": {}}}
	probes := 0
	o := rgOpts(f, "Foo")
	o.LookAstGrep = func() string { probes++; return "ast-grep" }
	Run(o)
	if probes != 0 {
		t.Error("no hits, no probe")
	}
	f.out["rg"] = rgOut("a.go:1:Foo()")
	Run(o)
	if probes != 1 {
		t.Errorf("probes = %d", probes)
	}
}
