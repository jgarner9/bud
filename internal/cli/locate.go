package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"jgarner.dev/bud/internal/locate"
)

const locateTextLimit = 6000

func init() {
	RegisterCommand(Locate())
}

// Locate finds source by a symbol or a literal phrase.
func Locate() *Command {
	var (
		out        toolOutput
		kind, lang string
		max        int
	)
	return &Command{
		Name:  "locate",
		Usage: "Find a symbol or phrase in local files: locate [flags] <query> [path...]",
		Flags: func(fs *flag.FlagSet) {
			out.Bind(fs, locateTextLimit)
			fs.StringVar(&kind, "kind", locate.KindAny, "which matches: def (definitions), use or any")
			fs.StringVar(&lang, "lang", "", "only this language: "+locate.LangNames())
			fs.IntVar(&max, "max", 20, "matches to show; the rest are only counted (0 for all)")
		},
		Run: func(g *Globals, args []string) error {
			return runLocate(g, args, kind, lang, max, out)
		},
	}
}

func runLocate(g *Globals, rest []string, kind, lang string, max int, out toolOutput) error {
	if len(rest) == 0 || rest[0] == "" {
		return usageErrorf("locate: no query; use `bud locate [flags] <symbol-or-phrase> [path...]` (put -- before a query that starts with -)")
	}
	if out.MaxChars < 0 {
		return usageErrorf("locate: --max-chars cannot be negative; use 0 for no text cap")
	}
	if max < 0 {
		return usageErrorf("locate: --max cannot be negative; use 0 for all matches")
	}
	switch kind {
	case locate.KindAny:
	case locate.KindDef, locate.KindUse:
		if !locate.IsSymbol(rest[0]) {
			return usageErrorf("locate: --kind %s needs a symbol (one identifier such as ParseConfig); a phrase has no definitions, so drop --kind", kind)
		}
	default:
		return usageErrorf("locate: --kind is def, use or any, not %q", kind)
	}
	langName := ""
	if lang != "" {
		var ok bool
		if langName, ok = locate.ParseLang(lang); !ok {
			return usageErrorf("locate: --lang %q is not supported; use one of: %s", lang, locate.LangNames())
		}
	}

	opts := locate.Options{Query: rest[0], Paths: rest[1:], Kind: kind, Lang: langName, Max: max}
	if p, err := lookPath("rg"); err == nil {
		opts.Tools.RG = p
	} else if p, err := lookPath("grep"); err == nil {
		opts.Tools.Grep = p
	}
	if locate.IsSymbol(opts.Query) {
		opts.LookAstGrep = func() string { return locate.FindAstGrep(lookPath, nil) }
	} else if p, err := lookPath("repomap"); err == nil {
		opts.Tools.Repomap = p
	}

	res, err := locate.Run(opts)
	if err != nil {
		return err
	}
	w := g.stdout
	if w == nil {
		w = os.Stdout
	}
	return out.Print(w, locateText(res, kind), res)
}

// locateText is the compact result: a header, any notes, then each file with
// its hits as `line [def] text`, and a last line for what the cap cut.
func locateText(r locate.Result, kind string) string {
	var b strings.Builder
	tools := strings.Join(r.Tools, ", ")
	switch {
	case r.Total == 0 && kind != locate.KindAny:
		fmt.Fprintf(&b, "locate %q: no %s found (%s); --kind any also shows the other kind", r.Query, kind, tools)
	case r.Total == 0:
		fmt.Fprintf(&b, "locate %q: no matches (%s)", r.Query, tools)
	case r.Symbol && kind == locate.KindAny:
		fmt.Fprintf(&b, "locate %q: %d def, %d use in %s (%s)", r.Query, r.Defs, r.Total-r.Defs, plural(r.Files, "file"), tools)
	default:
		label := "matches"
		if r.Symbol {
			label = kind
		}
		fmt.Fprintf(&b, "locate %q: %d %s in %s (%s)", r.Query, r.Total, label, plural(r.Files, "file"), tools)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "\nnote: %s", n)
	}
	width := 1
	for _, h := range r.Hits {
		width = max(width, len(fmt.Sprint(h.Line)))
	}
	prev := ""
	for _, h := range r.Hits {
		if h.File != prev {
			fmt.Fprintf(&b, "\n%s", h.File)
			prev = h.File
		}
		mark := "   "
		if h.Kind == locate.KindDef {
			mark = "def"
		}
		fmt.Fprintf(&b, "\n  %*d %s  %s", width, h.Line, mark, h.Text)
	}
	if r.Cut > 0 {
		cut := plural(r.Cut, "more match") + " cut"
		if r.Symbol && kind == locate.KindAny {
			cut = fmt.Sprintf("%s (%d def, %d use)", strings.Replace(cut, " cut", "", 1), r.CutDefs, r.Cut-r.CutDefs) + " cut"
		}
		fmt.Fprintf(&b, "\n[%s; show all with --max 0, or narrow with --kind, --lang or a path]", cut)
	}
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	if strings.HasSuffix(word, "ch") {
		return fmt.Sprintf("%d %ses", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
