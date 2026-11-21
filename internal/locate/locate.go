// Package locate finds where code lives for `bud locate`. ripgrep (or grep)
// finds every match; ast-grep, or a regex per language when it is missing,
// says which matches are definitions; a repo map, when one is installed, ranks
// the files for a phrase. The tools run through a Runner, so the ranking and
// shaping are testable without them.
package locate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Kinds of hit. A phrase has no definitions, so its hits are all "match".
const (
	KindDef   = "def"
	KindUse   = "use"
	KindMatch = "match"
	KindAny   = "any" // only as a filter
)

// Tools are the paths of the tools to use; an empty one is not installed.
type Tools struct {
	RG, Grep, AstGrep, Repomap string
}

// Options say what to find.
type Options struct {
	Query string
	Paths []string // searched in place of the working directory
	Kind  string   // def, use or any (the default)
	Lang  string   // a name from ParseLang, or ""
	Max   int      // hits to return; 0 for all
	Tools Tools
	// LookAstGrep finds ast-grep when it is first needed, that is, when there
	// are symbol hits to classify. Used when Tools.AstGrep is empty.
	LookAstGrep func() string
	Run         Runner
}

// Hit is one place a query matched.
type Hit struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Kind string `json:"kind"`
	Text string `json:"text"`
	raw  string // the line as found, indentation kept, for the definition regexes
}

// Result is what locate found. Hits are grouped by file; files and the hits
// chosen under the cap are in rank order, lines within a file ascending.
type Result struct {
	Query   string   `json:"query"`
	Symbol  bool     `json:"symbol"`
	Tools   []string `json:"tools"`
	Notes   []string `json:"notes"`
	Total   int      `json:"total"` // matches of the requested kind
	Defs    int      `json:"defs"`
	Files   int      `json:"files"`
	Hits    []Hit    `json:"hits"`
	Cut     int      `json:"cut"`      // matches beyond Max
	CutDefs int      `json:"cut_defs"` // of those, definitions
}

// maxHits stops one query from holding a whole tree in memory.
const maxHits = 20000

// maxText is how long a line of context may be.
const maxText = 160

// Run finds q and returns the ranked, capped result.
func Run(o Options) (Result, error) {
	run := o.Run
	if run == nil {
		run = ExecRunner
	}
	sym := IsSymbol(o.Query)
	res := Result{Query: o.Query, Symbol: sym, Notes: []string{}, Hits: []Hit{}, Tools: []string{}}

	hits, tool, err := search(run, o, sym, &res)
	if err != nil {
		return res, err
	}
	res.Tools = append(res.Tools, tool)

	if sym {
		defs := findDefs(run, o, hits, &res)
		for i := range hits {
			hits[i].Kind = KindUse
			if defs[key(hits[i].File, hits[i].Line)] {
				hits[i].Kind = KindDef
			}
		}
	} else {
		for i := range hits {
			hits[i].Kind = KindMatch
		}
	}
	if o.Kind == KindDef || o.Kind == KindUse {
		keep := hits[:0]
		for _, h := range hits {
			if h.Kind == o.Kind {
				keep = append(keep, h)
			}
		}
		hits = keep
	}

	rank := fileRank(run, o, sym, hits, &res)
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if (a.Kind == KindDef) != (b.Kind == KindDef) {
			return a.Kind == KindDef
		}
		if ta, tb := isTest(a.File), isTest(b.File); ta != tb {
			return !ta
		}
		if rank[a.File] != rank[b.File] {
			return rank[a.File] < rank[b.File]
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})

	res.Total = len(hits)
	files := map[string]bool{}
	for _, h := range hits {
		files[h.File] = true
		if h.Kind == KindDef {
			res.Defs++
		}
	}
	res.Files = len(files)
	if o.Max > 0 && len(hits) > o.Max {
		for _, h := range hits[o.Max:] {
			res.Cut++
			if h.Kind == KindDef {
				res.CutDefs++
			}
		}
		hits = hits[:o.Max]
	}
	res.Hits = groupByFile(hits)
	return res, nil
}

func key(file string, line int) string { return file + ":" + strconv.Itoa(line) }

// groupByFile puts each file's hits together, files in the order they first
// appear (their rank), lines ascending.
func groupByFile(hits []Hit) []Hit {
	order := []string{}
	by := map[string][]Hit{}
	for _, h := range hits {
		if _, ok := by[h.File]; !ok {
			order = append(order, h.File)
		}
		by[h.File] = append(by[h.File], h)
	}
	out := make([]Hit, 0, len(hits))
	for _, f := range order {
		hs := by[f]
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].Line < hs[j].Line })
		out = append(out, hs...)
	}
	return out
}

// isTest guesses from the path whether a file is test code, so tests rank
// after the code they test.
func isTest(p string) bool {
	base := filepath.Base(p)
	if strings.HasSuffix(base, "_test.go") || strings.HasPrefix(base, "test_") ||
		strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || strings.Contains(base, "_spec.") {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		switch part {
		case "test", "tests", "testdata", "__tests__", "spec":
			return true
		}
	}
	return false
}

// search runs ripgrep, or grep when ripgrep is missing, and parses the hits.
func search(run Runner, o Options, sym bool, res *Result) ([]Hit, string, error) {
	var (
		name string
		args []string
		tool string
	)
	lang := langByName(o.Lang)
	switch {
	case o.Tools.RG != "":
		name, tool = o.Tools.RG, "rg"
		args = []string{"--no-config", "--null", "--line-number", "--no-heading", "--with-filename", "--color", "never", "--fixed-strings"}
		if sym {
			args = append(args, "--word-regexp", "--case-sensitive")
		} else {
			args = append(args, "--smart-case")
		}
		if lang != nil {
			args = append(args, "--type", lang.rgType)
		}
		args = append(args, "--", o.Query)
		args = append(args, o.Paths...)
	case o.Tools.Grep != "":
		name, tool = o.Tools.Grep, "grep"
		res.Notes = append(res.Notes, "ripgrep not found; using grep -rn, which also searches ignored files; install ripgrep (brew install ripgrep)")
		args = []string{"-rnIFH"}
		if sym {
			args = append(args, "-w")
		} else if q := o.Query; q == strings.ToLower(q) {
			args = append(args, "-i") // smart case, as ripgrep does
		}
		args = append(args, "--", o.Query)
		if len(o.Paths) == 0 {
			args = append(args, ".")
		}
		args = append(args, o.Paths...)
	default:
		return nil, "", errors.New("neither ripgrep nor grep is on PATH; install ripgrep (brew install ripgrep)")
	}

	out, err := run(name, args...)
	if err != nil {
		return nil, tool, fmt.Errorf("run %s: %w", tool, err)
	}
	if out.Exit > 1 && len(bytes.TrimSpace(out.Stdout)) == 0 {
		msg := strings.TrimSpace(strings.SplitN(out.Stderr, "\n", 2)[0])
		if msg == "" {
			msg = "exit " + strconv.Itoa(out.Exit)
		}
		return nil, tool, fmt.Errorf("%s failed: %s; check the path arguments", tool, msg)
	}
	if out.Exit > 1 {
		res.Notes = append(res.Notes, tool+" could not read some files; results may be incomplete")
	}

	lines := bytes.Split(out.Stdout, []byte("\n"))
	if out.Truncated && len(lines) > 0 {
		lines = lines[:len(lines)-1] // the last line was cut off mid-way
	}
	var hits []Hit
	nonEmpty := 0
	for _, ln := range lines {
		if len(bytes.TrimSpace(ln)) > 0 {
			nonEmpty++
		}
		var h Hit
		var ok bool
		if tool == "grep" {
			h, ok = parseGrepHit(ln)
		} else {
			h, ok = parseHit(ln)
		}
		if !ok {
			continue
		}
		if tool == "grep" && lang != nil {
			fileLang := langOfFile(h.File)
			if fileLang == nil || fileLang.name != lang.name {
				continue
			}
		}
		if len(hits) == maxHits {
			out.Truncated = true
			break
		}
		hits = append(hits, h)
	}
	if nonEmpty > 0 && len(hits) == 0 {
		return nil, tool, fmt.Errorf("%s printed output bud could not read; check that %s is a standard ripgrep or grep", tool, name)
	}
	if out.Truncated {
		res.Notes = append(res.Notes, fmt.Sprintf("stopped reading at %d matches; narrow with a path, --lang or a longer query", len(hits)))
	}
	return hits, tool, nil
}

// parseHit reads "path\0line:text".
func parseHit(ln []byte) (Hit, bool) {
	nul := bytes.IndexByte(ln, 0)
	if nul < 0 {
		return Hit{}, false
	}
	rest := ln[nul+1:]
	colon := bytes.IndexByte(rest, ':')
	if colon < 0 {
		return Hit{}, false
	}
	n, err := strconv.Atoi(string(rest[:colon]))
	if err != nil {
		return Hit{}, false
	}
	raw := strings.TrimRight(string(rest[colon+1:]), "\r")
	if len(raw) > 2000 {
		raw = raw[:2000]
	}
	return Hit{File: filepath.Clean(string(ln[:nul])), Line: n, Text: oneLine(raw), raw: raw}, true
}

// parseGrepHit reads the portable grep format "path:line:text".
func parseGrepHit(ln []byte) (Hit, bool) {
	for i, b := range ln {
		if b != ':' {
			continue
		}
		end := i + 1
		for end < len(ln) && ln[end] >= '0' && ln[end] <= '9' {
			end++
		}
		if end == i+1 || end >= len(ln) || ln[end] != ':' {
			continue
		}
		n, err := strconv.Atoi(string(ln[i+1 : end]))
		if err != nil {
			return Hit{}, false
		}
		raw := strings.TrimRight(string(ln[end+1:]), "\r")
		if len(raw) > 2000 {
			raw = raw[:2000]
		}
		return Hit{File: filepath.Clean(string(ln[:i])), Line: n, Text: oneLine(raw), raw: raw}, true
	}
	return Hit{}, false
}

// oneLine trims a line of context and cuts it at maxText characters.
func oneLine(s string) string {
	s = strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == 0 })
	if r := []rune(s); len(r) > maxText {
		s = string(r[:maxText]) + "…"
	}
	return s
}

// findDefs returns the file:line keys of hits that declare the symbol: those
// ast-grep says declare it, plus those a regex for their language says do. The
// union means a rule ast-grep cannot run still leaves the regexes working.
func findDefs(run Runner, o Options, hits []Hit, res *Result) map[string]bool {
	defs := map[string]bool{}
	present := map[string]bool{}
	for _, h := range hits {
		if l := langOfFile(h.File); l != nil {
			present[l.name] = true
		}
	}

	astGrep := o.Tools.AstGrep
	if astGrep == "" && o.LookAstGrep != nil && len(present) > 0 {
		astGrep = o.LookAstGrep() // only probed when there is something to classify
	}
	if astGrep != "" {
		paths := o.Paths
		if len(paths) == 0 {
			paths = []string{"."}
		}
		worked, failed := false, ""
		for _, l := range languages {
			if !present[l.name] {
				continue
			}
			for _, al := range l.astLangs {
				args := append([]string{"scan", "--inline-rules", astRule(&l, al, o.Query), "--json=stream", "--"}, paths...)
				out, err := run(astGrep, args...)
				switch {
				case err != nil:
					failed = err.Error()
				case out.Truncated:
					failed = "output truncated"
				case out.Exit != 0:
					failed = strings.TrimSpace(strings.SplitN(out.Stderr, "\n", 2)[0])
					if failed == "" {
						failed = "exit " + strconv.Itoa(out.Exit)
					}
				default:
					worked = true
					for _, k := range parseAstGrep(out.Stdout) {
						defs[k] = true
					}
				}
			}
		}
		if worked {
			res.Tools = append(res.Tools, "ast-grep")
		}
		if failed != "" {
			res.Notes = append(res.Notes, "ast-grep failed ("+failed+"); definitions may rely on regex heuristics")
		}
	}

	re := matchers(o.Query)
	for _, h := range hits {
		if l := langOfFile(h.File); l != nil {
			for _, m := range re[l.name] {
				if m.MatchString(h.raw) {
					defs[key(h.File, h.Line)] = true
					break
				}
			}
		}
	}
	return defs
}

// parseAstGrep reads ast-grep's --json=stream output (a JSON array also
// parses) into file:line keys, lines made 1-based.
func parseAstGrep(b []byte) []string {
	type match struct {
		File  string `json:"file"`
		Range struct {
			Start struct {
				Line int `json:"line"`
			} `json:"start"`
		} `json:"range"`
	}
	var ms []match
	if err := json.Unmarshal(bytes.TrimSpace(b), &ms); err != nil {
		ms = ms[:0]
		for _, ln := range bytes.Split(b, []byte("\n")) {
			var m match
			if len(bytes.TrimSpace(ln)) > 0 && json.Unmarshal(ln, &m) == nil && m.File != "" {
				ms = append(ms, m)
			}
		}
	}
	keys := make([]string, 0, len(ms))
	for _, m := range ms {
		keys = append(keys, key(filepath.Clean(m.File), m.Range.Start.Line+1))
	}
	return keys
}

// fileRank orders files for a phrase by where a repo map lists them (earlier
// means more central). Without a map, or for a symbol, every file ties and
// the path order decides. The map's format is not assumed: a file counts at
// the first place its path appears in the output.
func fileRank(run Runner, o Options, sym bool, hits []Hit, res *Result) map[string]int {
	rank := map[string]int{}
	if sym || o.Tools.Repomap == "" {
		return rank
	}
	files := map[string]bool{}
	for _, h := range hits {
		files[h.File] = true
	}
	if len(files) < 2 {
		return rank
	}
	root := "."
	if len(o.Paths) == 1 {
		root = o.Paths[0]
	}
	out, err := run(o.Tools.Repomap, "-root", root, "-max", "200")
	if err != nil || out.Exit != 0 || len(bytes.TrimSpace(out.Stdout)) == 0 {
		res.Notes = append(res.Notes, "repomap gave no map; files are in path order")
		return rank
	}
	// A file ranks by the first whole token of the output that names it, either
	// as given or relative to the root. Substrings do not count (a.go is not data.go).
	first := map[string]int{}
	for i, tok := range strings.Fields(string(out.Stdout)) {
		tok = strings.Trim(tok, "\"'`,;:()[]")
		if tok == "" {
			continue
		}
		for _, c := range []string{filepath.Clean(tok), filepath.Join(root, tok)} {
			if _, seen := first[c]; !seen {
				first[c] = i
			}
		}
	}
	for f := range files {
		if i, ok := first[f]; ok {
			rank[f] = i
		} else {
			rank[f] = 1 << 30
		}
	}
	res.Tools = append(res.Tools, "repomap")
	return rank
}
