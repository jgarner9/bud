package locate

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

// language is what locate knows about one language: how to spot its files, how
// to ask ripgrep and grep for them, a regex fallback for definitions and the
// ast-grep node kinds that declare a name.
type language struct {
	name     string
	aliases  []string
	exts     []string
	rgType   string   // ripgrep's own file type, which keeps .gitignore in force
	astLangs []string // ast-grep language names the rule is run for
	astKinds []string // node kinds with a `name` field that declare something
	defs     []string // regex fallbacks; {S} is the symbol, {E} a word end
	callDefs []string // like defs, but shaped like a call, so skipped for control-flow words
}

var languages = []language{
	{
		name: "go", exts: []string{".go"}, rgType: "go", astLangs: []string{"go"},
		astKinds: []string{"function_declaration", "method_declaration", "type_spec", "type_alias", "const_spec", "var_spec"},
		defs: []string{
			`^\s*func\s+(\([^)]*\)\s*)?{S}{E}`,
			`^\s*type\s+{S}{E}`,
			`^\s*(var|const)\s+{S}{E}`,
		},
	},
	{
		name: "lua", exts: []string{".lua"}, rgType: "lua", astLangs: []string{"lua"},
		astKinds: []string{"function_declaration"},
		defs: []string{
			`^\s*(local\s+)?function\s+([\w.]+[.:])?{S}{E}`,
			`^\s*(local\s+)?([\w.]+\.)?{S}\s*=\s*function{E}`,
			`^\s*local\s+{S}{E}`,
		},
	},
	{
		name: "python", aliases: []string{"py"}, exts: []string{".py", ".pyi"}, rgType: "py", astLangs: []string{"python"},
		astKinds: []string{"function_definition", "class_definition"},
		defs: []string{
			`^\s*(async\s+)?def\s+{S}{E}`,
			`^\s*class\s+{S}{E}`,
			`^{S}\s*(:[^=]*)?=`,
		},
	},
	{
		name: "typescript", aliases: []string{"ts"}, exts: []string{".ts", ".tsx", ".mts", ".cts"}, rgType: "ts", astLangs: []string{"typescript", "tsx"},
		astKinds: []string{"function_declaration", "generator_function_declaration", "class_declaration", "abstract_class_declaration",
			"method_definition", "interface_declaration", "type_alias_declaration", "enum_declaration", "variable_declarator"},
		defs: jsDefs, callDefs: jsCallDefs,
	},
	{
		name: "javascript", aliases: []string{"js"}, exts: []string{".js", ".jsx", ".mjs", ".cjs"}, rgType: "js", astLangs: []string{"javascript"},
		astKinds: []string{"function_declaration", "generator_function_declaration", "class_declaration", "method_definition", "variable_declarator"},
		defs:     jsDefs, callDefs: jsCallDefs,
	},
	{
		name: "rust", aliases: []string{"rs"}, exts: []string{".rs"}, rgType: "rust", astLangs: []string{"rust"},
		astKinds: []string{"function_item", "function_signature_item", "struct_item", "enum_item", "union_item", "trait_item",
			"type_item", "const_item", "static_item", "mod_item", "macro_definition"},
		defs: []string{
			`^\s*(pub(\([^)]*\))?\s+)?((const|async|unsafe|default)\s+|extern\s+("[^"]*"\s+)?)*(fn|struct|enum|trait|type|const|static|mod|union)\s+(mut\s+)?{S}{E}`,
			`^\s*macro_rules!\s+{S}{E}`,
		},
	},
	{
		name: "shell", aliases: []string{"sh", "bash"}, exts: []string{".sh", ".bash", ".zsh"}, rgType: "sh", astLangs: []string{"bash"},
		astKinds: []string{"function_definition"},
		defs: []string{
			`^\s*function\s+{S}{E}`,
			`^\s*{S}\s*\(\)`,
			`^\s*(export\s+|readonly\s+|declare\s+\S+\s+)?{S}=`,
		},
	},
}

var jsDefs = []string{
	`^\s*(export\s+)?(default\s+)?(async\s+)?function\*?\s+{S}{E}`,
	`^\s*(export\s+)?(default\s+)?(abstract\s+)?class\s+{S}{E}`,
	`^\s*(export\s+)?(declare\s+)?(const|let|var)\s+{S}{E}`,
	`^\s*(export\s+)?(declare\s+)?(interface|type|enum)\s+{S}{E}`,
}

// jsCallDefs are methods, which look like `name(args) {`; for `if`, `catch` and
// the other keywords that read the same way they are not definitions.
var jsCallDefs = []string{
	`^\s*((public|private|protected|static|async|readonly|override)\s+)*{S}\s*\(.*\)\s*(:\s*[^=]+)?\{\s*$`,
}

var controlWords = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true,
	"function": true, "return": true, "else": true, "with": true}

// ParseLang resolves a --lang value (a name or alias, any case).
func ParseLang(s string) (string, bool) {
	s = strings.ToLower(s)
	for _, l := range languages {
		if s == l.name {
			return l.name, true
		}
		for _, a := range l.aliases {
			if s == a {
				return l.name, true
			}
		}
	}
	return "", false
}

// LangNames lists the --lang names, for error messages.
func LangNames() string {
	names := make([]string, len(languages))
	for i, l := range languages {
		names[i] = l.name
	}
	return strings.Join(names, ", ")
}

func langByName(name string) *language {
	for i := range languages {
		if languages[i].name == name {
			return &languages[i]
		}
	}
	return nil
}

func langOfFile(path string) *language {
	ext := strings.ToLower(filepath.Ext(path))
	for i := range languages {
		for _, e := range languages[i].exts {
			if e == ext {
				return &languages[i]
			}
		}
	}
	return nil
}

// symbolRE is what counts as a symbol: one identifier. Anything else is a
// phrase, searched as literal text.
var symbolRE = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// IsSymbol reports whether q is a single identifier.
func IsSymbol(q string) bool { return symbolRE.MatchString(q) }

// matchers compiles the regex fallbacks for sym, per language name.
func matchers(sym string) map[string][]*regexp.Regexp {
	q := regexp.QuoteMeta(sym)
	m := map[string][]*regexp.Regexp{}
	for _, l := range languages {
		tpls := l.defs
		if !controlWords[sym] {
			tpls = append(append([]string{}, l.defs...), l.callDefs...)
		}
		for _, tpl := range tpls {
			re := strings.NewReplacer("{S}", q, "{E}", `(?:[^\w$]|$)`).Replace(tpl)
			m[l.name] = append(m[l.name], regexp.MustCompile(re))
		}
	}
	return m
}

// astRule is the inline ast-grep rule that finds the names of declarations of
// sym: an identifier-like node that is the `name` field of a declaring kind and
// ends in sym (so Lua's `M.sym` and `M:sym` count). The match is the name node,
// so its line is the line ripgrep reports even when a decorator or a wrapped
// signature puts the declaration's start elsewhere. Built with encoding/json,
// never by hand.
func astRule(l *language, astLang, sym string) string {
	kinds := make([]any, len(l.astKinds))
	for i, k := range l.astKinds {
		kinds[i] = map[string]any{"kind": k}
	}
	rule := map[string]any{
		"id": "locate-def", "language": astLang, "severity": "hint", "message": "definition",
		"rule": map[string]any{
			"regex":  `(^|[.:])` + regexp.QuoteMeta(sym) + `$`,
			"inside": map[string]any{"field": "name", "stopBy": "neighbor", "any": kinds},
		},
	}
	b, _ := json.Marshal(rule)
	return string(b)
}
