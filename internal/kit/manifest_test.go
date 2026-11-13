package kit

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud"
)

func TestTheEmbeddedKitLoadsWithEveryServerClodHad(t *testing.T) {
	k, err := Load(bud.Kit())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"basic-memory", "cocoindex-code", "code-index", "context7", "exa", "github", "headroom", "linear", "playwright", "serena"} {
		if _, ok := k.Servers[name]; !ok {
			t.Errorf("no server %s in %v", name, sortedNames(k.Servers))
		}
	}
}

func TestExpectedServersAreTheOnesOnByDefaultThatNeedNoCommand(t *testing.T) {
	k, err := Load(bud.Kit())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := k.ExpectedServers(), []string{"basic-memory", "context7", "exa"}; !slices.Equal(got, want) {
		t.Errorf("ExpectedServers() = %v, want %v", got, want)
	}
}

func TestNoFileInTheEmbeddedKitNamesAPersonATailnetOrClod(t *testing.T) {
	kitFS := bud.Kit()
	err := fs.WalkDir(kitFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(kitFS, path)
		if err != nil {
			return err
		}
		text := strings.ToLower(string(data))
		for _, word := range []string{"jason", "tailnet", "ts.net", "clod"} {
			if strings.Contains(text, word) {
				t.Errorf("kit/%s contains %q: the kit is public and holds no personal detail", path, word)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// server is a valid server entry; the cases below break one thing in it
const server = `{"description": "d", "mcp": {"type": "http", "url": "https://x.example/mcp"}, "modes": ["full"]}`

func TestLoadChecksTheManifest(t *testing.T) {
	files := fstest.MapFS{
		"rules/ok.md": {Data: []byte("a rule\n")},
		"empty.md":    {Data: nil},
	}
	cases := []struct {
		name string
		json string
		want string // what the error contains; "" means it loads
	}{
		{"a valid manifest loads", `{"servers": {"a": ` + server + `}}`, ""},
		{"an empty manifest loads", `{}`, ""},
		{"invalid JSON", `{`, "kit.json: "},
		{"an unknown top-level key", `{"core": "x"}`, `kit.json: json: unknown field "core"`},
		{"a bad server name", `{"servers": {"A": ` + server + `}}`, `kit.json: server "A": the name must match`},
		{"an unknown server field", `{"servers": {"a": {"description": "d", "extra": 1}}}`, `kit.json: server "a": json: unknown field "extra"`},
		{"an unknown mcp field", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "x": 1}}}}`, `kit.json: server "a": json: unknown field "x"`},
		{"an unknown vars field", `{"servers": {"a": {"description": "d", "vars": {"A": {"x": 1}}}}}`, `kit.json: server "a": json: unknown field "x"`},
		{"no description", `{"servers": {"a": {"mcp": {"type": "http", "url": "u"}, "modes": []}}}`, `kit.json: server "a": description`},
		{"no mcp", `{"servers": {"a": {"description": "d", "modes": []}}}`, `kit.json: server "a": mcp is missing`},
		{"no modes", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u"}}}}`, `kit.json: server "a": modes is missing`},
		{"null modes", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u"}, "modes": null}}}`, `kit.json: server "a": modes is missing`},
		{"empty modes are opt-in", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u"}, "modes": []}}}`, ""},
		{"an unknown mcp type", `{"servers": {"a": {"description": "d", "mcp": {"type": "ws", "url": "u"}, "modes": []}}}`, `kit.json: server "a": mcp: type "ws"`},
		{"stdio without a command", `{"servers": {"a": {"description": "d", "mcp": {"type": "stdio"}, "modes": []}}}`, `kit.json: server "a": mcp: a stdio server needs a command`},
		{"stdio with a url", `{"servers": {"a": {"description": "d", "mcp": {"type": "stdio", "command": "c", "url": "u"}, "modes": []}}}`, `kit.json: server "a": mcp: a stdio server has no url`},
		{"stdio with headers", `{"servers": {"a": {"description": "d", "mcp": {"type": "stdio", "command": "c", "headers": {"h": "v"}}, "modes": []}}}`, `kit.json: server "a": mcp: a stdio server has no url or headers`},
		{"http without a url", `{"servers": {"a": {"description": "d", "mcp": {"type": "http"}, "modes": []}}}`, `kit.json: server "a": mcp: an http server needs a url`},
		{"sse with a command", `{"servers": {"a": {"description": "d", "mcp": {"type": "sse", "url": "u", "command": "c"}, "modes": []}}}`, `kit.json: server "a": mcp: an sse server has no command`},
		{"http with env", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u", "env": {"E": "v"}}, "modes": []}}}`, `kit.json: server "a": mcp: an http server has no command, args or env`},
		{"an unknown mode", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u"}, "modes": ["lite"]}}}`, `kit.json: server "a": mode "lite"`},
		{"a repeated mode", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u"}, "modes": ["full", "full"]}}}`, `kit.json: server "a": mode "full" is listed twice`},
		{"requires with a space", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u"}, "modes": [], "requires": "a b"}}}`, `kit.json: server "a": requires "a b"`},
		{"requires with a slash", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u"}, "modes": [], "requires": "/bin/a"}}}`, `kit.json: server "a": requires "/bin/a"`},
		{"connector on a stdio server", `{"servers": {"a": {"description": "d", "mcp": {"type": "stdio", "command": "c"}, "modes": [], "connector": "A"}}, "account": {"connectors": {"A": {"on": true}}}}`, `kit.json: server "a": connector and desktop`},
		{"desktop on a stdio server", `{"servers": {"a": {"description": "d", "mcp": {"type": "stdio", "command": "c"}, "modes": [], "desktop": true}}}`, `kit.json: server "a": connector and desktop`},
		{"a lowercase var name", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u"}, "modes": [], "vars": {"key": {"prompt": "p", "secret": true}}}}}`, `kit.json: server "a": var "key"`},
		{"an empty var prompt", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "${K}"}, "modes": [], "vars": {"K": {"prompt": "", "secret": true}}}}}`, `kit.json: server "a": var "K": prompt is empty`},
		{"a var without secret", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "${K}"}, "modes": [], "vars": {"K": {"prompt": "p"}}}}}`, `kit.json: server "a": var "K": secret is missing`},
		{"a var with secret false loads", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "${K}"}, "modes": [], "vars": {"K": {"prompt": "p", "secret": false}}}}}`, ""},
		{"a malformed placeholder", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "${K"}, "modes": []}}}`, `kit.json: server "a": mcp url`},
		{"a lowercase placeholder", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "${k}"}, "modes": []}}}`, `kit.json: server "a": mcp url`},
		{"a placeholder not in vars", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "${K}"}, "modes": []}}}`, `kit.json: server "a": mcp url: placeholder K is not in vars`},
		{"a required placeholder loads", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u", "headers": {"h": "Bearer ${K:?}"}}, "modes": [], "vars": {"K": {"prompt": "p", "secret": true}}}}}`, ""},
		{"a var no placeholder uses", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u"}, "modes": [], "vars": {"K": {"prompt": "p", "secret": true}}}}}`, `kit.json: server "a": var "K" is not used`},
		{"a placeholder in a header name", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u", "headers": {"${K}": "v"}}, "modes": [], "vars": {"K": {"prompt": "p", "secret": true}}}}}`, `kit.json: server "a": mcp headers: the name`},
		{"a placeholder in an env name", `{"servers": {"a": {"description": "d", "mcp": {"type": "stdio", "command": "c", "env": {"${K}": "v"}}, "modes": [], "vars": {"K": {"prompt": "p", "secret": true}}}}}`, `kit.json: server "a": mcp env: the name`},
		{"server settings that aren't an object", `{"servers": {"a": {"description": "d", "mcp": {"type": "http", "url": "u"}, "modes": [], "settings": []}}}`, `kit.json: server "a": settings: must be an object`},
		{"settings that aren't an object", `{"settings": "x"}`, `kit.json: settings: must be an object`},
		{"settings may hold any key", `{"settings": {"anything": [1]}}`, ""},
		{"a connector with a trailing space", `{"account": {"connectors": {"Linear ": {"on": true}}}}`, `kit.json: connector "Linear "`},
		{"an empty connector name", `{"account": {"connectors": {"": {"on": true}}}}`, `kit.json: connector ""`},
		{"an unknown connector field", `{"account": {"connectors": {"Linear": {"on": true, "x": 1}}}}`, `kit.json: connector "Linear": json: unknown field "x"`},
		{"a connector without on", `{"account": {"connectors": {"Linear": {}}}}`, `kit.json: connector "Linear": on is missing`},
		{"a server connector that isn't in the account", `{"servers": {"a": ` + strings.TrimSuffix(server, "}") + `, "connector": "Linear"}}}`, `kit.json: server "a": connector "Linear" is not in account.connectors`},
		{"two servers with one connector", `{"servers": {"a": ` + strings.TrimSuffix(server, "}") + `, "connector": "L"}, "b": ` + strings.TrimSuffix(server, "}") + `, "connector": "L"}}, "account": {"connectors": {"L": {"on": true}}}}`, `kit.json: server "b": connector "L" is already`},
		{"an unknown account field", `{"account": {"x": 1}}`, `kit.json: account: json: unknown field "x"`},
		{"a wanted plugin with an @", `{"plugins": ["a@m"], "account": {"plugins": {"wanted": ["a@m"]}}}`, `kit.json: account: wanted plugin "a@m"`},
		{"an unwanted plugin with an @", `{"account": {"plugins": {"unwanted": ["a@m"]}}}`, `kit.json: account: unwanted plugin "a@m"`},
		{"a plugin both wanted and unwanted", `{"plugins": ["a@m"], "account": {"plugins": {"wanted": ["a"], "unwanted": ["a"]}}}`, `kit.json: account: plugin "a" is both`},
		{"a wanted plugin the kit lacks", `{"account": {"plugins": {"wanted": ["a"]}}}`, `kit.json: account: wanted plugin "a" is not in plugins`},
		{"a wanted plugin the kit has loads", `{"plugins": ["a@m"], "account": {"plugins": {"wanted": ["a"], "unwanted": ["b"]}}}`, ""},
		{"an empty account marketplace", `{"account": {"marketplaces": [""]}}`, `kit.json: account: marketplaces has an empty entry`},
		{"a repeated account marketplace", `{"account": {"marketplaces": ["o/r", "o/r"]}}`, `kit.json: account: marketplace "o/r" is listed twice`},
		{"instructions without a path", `{"marketplaces": {"m": "x"}, "account": {"instructions": "m"}}`, `kit.json: account: instructions`},
		{"instructions from an unknown marketplace", `{"account": {"instructions": "m/core.md"}}`, `kit.json: account: instructions: "m/core.md": "m" is not in marketplaces`},
		{"instructions with a bad path", `{"marketplaces": {"m": "x"}, "account": {"instructions": "m/../core.md"}}`, `kit.json: account: instructions`},
		{"instructions that are valid", `{"marketplaces": {"m": "x"}, "account": {"instructions": "m/core.md"}}`, ""},
		{"a bad rule name", `{"rules": {"Notes.md": {"file": "rules/ok.md"}}}`, `kit.json: rule "Notes.md": the name must match`},
		{"a rule name without .md", `{"rules": {"notes": {"file": "rules/ok.md"}}}`, `kit.json: rule "notes": the name must match`},
		{"an unknown rule field", `{"rules": {"a.md": {"file": "rules/ok.md", "x": 1}}}`, `kit.json: rule "a.md": json: unknown field "x"`},
		{"a rule with neither source", `{"rules": {"a.md": {}}}`, `kit.json: rule "a.md": exactly one`},
		{"a rule with both sources", `{"marketplaces": {"m": "x"}, "rules": {"a.md": {"file": "rules/ok.md", "marketplace": "m/a.md"}}}`, `kit.json: rule "a.md": exactly one`},
		{"a rule file that is missing", `{"rules": {"a.md": {"file": "rules/none.md"}}}`, `kit.json: rule "a.md": file:`},
		{"a rule file that is empty", `{"rules": {"a.md": {"file": "empty.md"}}}`, `kit.json: rule "a.md": file: empty.md is empty`},
		{"a rule file with an invalid path", `{"rules": {"a.md": {"file": "../a.md"}}}`, `kit.json: rule "a.md": file: "../a.md" is not a valid path`},
		{"a rule from an unknown marketplace", `{"rules": {"a.md": {"marketplace": "m/a.md"}}}`, `kit.json: rule "a.md": marketplace:`},
		{"a rule for an unknown server", `{"rules": {"a.md": {"file": "rules/ok.md", "server": "nope"}}}`, `kit.json: rule "a.md": server "nope"`},
		{"a rule for a server the kit has", `{"servers": {"a": ` + server + `}, "rules": {"a.md": {"file": "rules/ok.md", "server": "a"}}}`, ""},
		{"an unknown cloud field", `{"cloud": {"x": 1}}`, `kit.json: cloud: json: unknown field "x"`},
		{"a cloud script that is missing", `{"cloud": {"script": "cloud/none.sh"}}`, `kit.json: cloud: script:`},
		{"a cloud script that is empty", `{"cloud": {"script": "empty.md"}}`, `kit.json: cloud: script: empty.md is empty`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{"kit.json": {Data: []byte(c.json)}}
			for name, f := range files {
				fsys[name] = f
			}
			_, err := Load(fsys)
			switch {
			case c.want == "" && err != nil:
				t.Errorf("Load error %v, want none", err)
			case c.want != "" && (err == nil || !strings.HasPrefix(err.Error(), c.want[:min(len(c.want), len("kit.json: "))]) || !strings.Contains(err.Error(), c.want)):
				t.Errorf("Load error %v, want one with %q", err, c.want)
			}
		})
	}
}

func TestLoadReportsTheFirstErrorInTheSameOrderEveryTime(t *testing.T) {
	data := `{"servers": {"b": {"description": "d"}, "a": {"description": "d"}}}`
	for range 20 {
		_, err := Load(fstest.MapFS{"kit.json": {Data: []byte(data)}})
		if err == nil || !strings.HasPrefix(err.Error(), `kit.json: server "a":`) {
			t.Fatalf("Load error %v, want one naming server a first", err)
		}
	}
}
