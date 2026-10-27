package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/kit"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/projects"
	"jgarner.dev/bud/internal/updater"
)

func init() {
	RegisterCommand(Completion())
	RegisterCommand(&Command{
		Name:  "__complete",
		Usage: "completion candidates for the shell scripts: __complete <words...> <word being typed>",
		Run:   func(_ *Globals, args []string) error { return complete(os.Stdout, CommandRegistry, args) },
	})
}

// Completion prints the completion script for a shell (JGA-96). Each script
// is a few lines that ask bud __complete, which reads the command registry,
// so new commands and flags complete without the scripts changing
func Completion() *Command {
	return &Command{
		Name:  "completion",
		Usage: "Prints shell completion: completion bash|zsh|fish (add eval \"$(bud completion zsh)\" to your shell rc)",
		Run: func(_ *Globals, args []string) error {
			if len(args) != 1 {
				return usageErrorf("completion takes bash, zsh or fish")
			}
			script, ok := completionScripts[args[0]]
			if !ok {
				return usageErrorf("completion: %q isn't bash, zsh or fish", args[0])
			}
			fmt.Print(script)
			return nil
		},
	}
}

var completionScripts = map[string]string{
	"bash": `# bud completion for bash: eval "$(bud completion bash)" in ~/.bashrc
_bud() {
  # the slice before IFS changes: bash 3.2 joins a sliced array on IFS
  local words=("${COMP_WORDS[@]:1:COMP_CWORD-1}")
  local IFS=$'\n'
  COMPREPLY=($(bud __complete "${words[@]}" "${COMP_WORDS[COMP_CWORD]}" 2>/dev/null))
}
complete -o default -F _bud bud
`,
	"zsh": `#compdef bud
# bud completion for zsh: source <(bud completion zsh), or save as _bud on $fpath
_bud() {
  local -a candidates
  candidates=("${(@f)$(bud __complete "${(@)words[2,CURRENT-1]}" "${words[CURRENT]}" 2>/dev/null)}")
  if (( ${#candidates} )) && [[ -n "${candidates[1]}" ]]; then
    compadd -a candidates
  else
    _files
  fi
}
if (( $+functions[compdef] )); then compdef _bud bud; fi
`,
	"fish": `# bud completion for fish: bud completion fish > ~/.config/fish/completions/bud.fish
function __bud_complete
    set -l words (commandline -opc)
    bud __complete $words[2..-1] (commandline -ct) 2>/dev/null
end
complete -c bud -f -a '(__bud_complete)'
complete -c bud -F -n 'test -z "(__bud_complete)"'
`,
}

// valueFlags are flags whose next word is their value, with what it can be
var valueFlags = map[string]func() []string{
	"layout":   layoutNames,
	"profile":  func() []string { return config.Profiles },
	"terminal": func() []string { return config.Terminals },
	"channel":  func() []string { return updater.Channels },
}

// complete writes the candidates for the last of args (the word being
// typed; "" for a new one), given the words before it. Nothing written means
// the shell falls back to file names (bud ., bud file.go)
func complete(w io.Writer, cmds map[string]*Command, args []string) error {
	if len(args) == 0 {
		args = []string{""}
	}
	words, cur := args[:len(args)-1], args[len(args)-1]
	var out []string

	// the flag before this word takes a value
	if n := len(words); n > 0 {
		if name, ok := flagName(words[n-1]); ok && !strings.Contains(words[n-1], "=") {
			if values, ok := valueFlags[name]; ok {
				return printMatches(w, values(), cur)
			}
		}
	}

	// walk past global flags to the command and its subcommand
	var cmd, sub *Command
	var positional []string
	for i := 0; i < len(words); i++ {
		word := words[i]
		if name, ok := flagName(word); ok {
			if _, takes := valueFlags[name]; takes && !strings.Contains(word, "=") {
				i++
			}
			continue
		}
		switch {
		case cmd == nil:
			cmd = cmds[word]
			if cmd == nil {
				return nil // a path: files
			}
		case sub == nil && len(cmd.Subs) > 0:
			for _, s := range cmd.Subs {
				if s.Name == word {
					sub = s
				}
			}
		default:
			positional = append(positional, word)
		}
	}

	switch {
	case strings.HasPrefix(cur, "-"):
		fs := flag.NewFlagSet("bud", flag.ContinueOnError)
		switch {
		case sub != nil && sub.Flags != nil:
			sub.Flags(fs)
		case sub == nil && cmd != nil && cmd.Flags != nil:
			cmd.Flags(fs)
		case cmd == nil:
			(&Globals{}).flags(fs)
		}
		fs.VisitAll(func(f *flag.Flag) {
			if len(f.Name) > 1 {
				out = append(out, "--"+f.Name)
			}
		})
	case cmd == nil:
		for name := range cmds {
			if !strings.HasPrefix(name, "__") {
				out = append(out, name)
			}
		}
	case len(cmd.Subs) > 0 && sub == nil:
		for _, s := range cmd.Subs {
			out = append(out, s.Name)
		}
	default:
		name := cmd.Name
		if sub != nil {
			name += " " + sub.Name
		}
		out = argValues(name, positional)
	}

	return printMatches(w, out, cur)
}

// argValues are the names a command's arguments can be
func argValues(cmd string, before []string) []string {
	switch cmd {
	case "lang enable", "lang disable", "lang rm":
		return languageNames()
	case "layout set-default", "layout set":
		if len(before) == 0 {
			return layoutNames()
		}
	case "tools enable", "tools disable", "tools rm", "tools promote":
		return toolNames()
	case "kit rm", "kit enable", "kit disable":
		return kitNames()
	case "kit add":
		if len(before) == 0 {
			return []string{"mcp", "skill", "agent", "command", "plugin"}
		}
	case "kit secret":
		if len(before) == 0 {
			return []string{"set", "rm"}
		}
	case "config get", "config set", "config unset":
		if len(before) == 0 {
			return configKeys()
		}
	case "projects rm", "projects pin", "projects unpin":
		return projectNames()
	case "completion":
		return []string{"bash", "zsh", "fish"}
	}

	return nil
}

func printMatches(w io.Writer, candidates []string, prefix string) error {
	var buf bytes.Buffer
	sort.Strings(candidates)
	for _, c := range slices.Compact(candidates) {
		if strings.HasPrefix(c, prefix) {
			buf.WriteString(c + "\n")
		}
	}
	_, err := w.Write(buf.Bytes())

	return err
}

// the dynamic names: all read from files, none run nvim, since completion has to be instant
func layoutNames() []string {
	p, err := paths.Resolve()
	if err != nil {
		return nil
	}
	layouts, _ := listLayouts(p)
	var out []string
	for _, l := range layouts {
		out = append(out, l.Name)
	}

	return out
}

func languageNames() []string {
	p, err := paths.Resolve()
	if err != nil {
		return nil
	}
	var out []string
	for _, dir := range []string{p.LanguageSpecs(), p.UserLanguageSpecs()} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if name, ok := strings.CutSuffix(e.Name(), ".lua"); ok && !strings.HasPrefix(name, "_") {
				out = append(out, name)
			}
		}
	}

	return out
}

func toolNames() []string {
	p, err := paths.Resolve()
	if err != nil {
		return nil
	}
	tools, _ := findTools(p)
	var out []string
	for _, t := range tools {
		out = append(out, t.Name)
	}

	return out
}

func kitNames() []string {
	k, err := kit.Load(kitFS())
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range k.Entries() {
		out = append(out, e[1])
	}

	return out
}

func configKeys() []string {
	data, _ := json.Marshal(config.Defaults())
	var tree map[string]any
	_ = json.Unmarshal(data, &tree)
	var out []string
	var walk func(m map[string]any, prefix string)
	walk = func(m map[string]any, prefix string) {
		for k, v := range m {
			if child, ok := v.(map[string]any); ok {
				walk(child, prefix+k+".")
				continue
			}
			out = append(out, prefix+k)
		}
	}
	walk(tree, "")

	return out
}

func projectNames() []string {
	p, err := paths.Resolve()
	if err != nil {
		return nil
	}
	r, err := projects.Load(p.Projects())
	if err != nil {
		return nil
	}
	var out []string
	for _, pr := range r.Sorted() {
		out = append(out, pr.Name)
	}

	return out
}
