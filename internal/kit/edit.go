package kit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Kinds are the kinds of kit entry, as kit.json names their lists
var Kinds = map[string]string{"mcp": "mcp", "skill": "skills", "agent": "agents", "command": "commands", "plugin": "plugins", "output-style": "output-styles"}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(@[a-z0-9][a-z0-9._-]*)?$`)

// Repo is a kit/ dir in a bud checkout, which add, rm and adopt edit
type Repo struct {
	Dir string // <checkout>/kit
}

func (r Repo) index() (map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(r.Dir, "kit.json"))
	if err != nil {
		return nil, err
	}
	var idx map[string]any
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("kit.json: %w", err)
	}

	return idx, nil
}

func (r Repo) saveIndex(idx map[string]any) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(r.Dir, "kit.json"), append(data, '\n'), 0o644)
}

func list(idx map[string]any, key string) []string {
	raw, _ := idx[key].([]any)
	var out []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

func setList(idx map[string]any, key string, names []string) {
	out := make([]any, len(names))
	for i, n := range names {
		out[i] = n
	}
	idx[key] = out
}

// Find says which kind an entry in kit.json is, or "" when there's none
func (r Repo) Find(name string) (string, error) {
	idx, err := r.index()
	if err != nil {
		return "", err
	}
	for kind, key := range Kinds {
		if slices.Contains(list(idx, key), name) {
			return kind, nil
		}
	}

	return "", nil
}

// paths are an entry's files, relative to the kit dir (a plugin has none:
// it's just its name in kit.json)
func entryPath(kind, name string) string {
	switch kind {
	case "plugin":
		return ""
	case "mcp":
		return "mcp/" + name + ".json"
	case "skill":
		return "skills/" + name
	case "agent":
		return "agents/" + name + ".md"
	case "output-style":
		return "output-styles/" + name + ".md"
	}

	return "commands/" + name + ".md"
}

// add writes files for an entry and lists it in kit.json
func (r Repo) add(kind, name string, files map[string][]byte) (string, error) {
	if !namePattern.MatchString(name) {
		return "", fmt.Errorf("%q isn't a usable name: lowercase letters, digits, dots and dashes", name)
	}
	if have, err := r.Find(name); err != nil {
		return "", err
	} else if have != "" {
		return "", fmt.Errorf("the kit already has a %s called %s", have, name)
	}
	for rel, data := range files {
		p := filepath.Join(r.Dir, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err == nil {
			return "", fmt.Errorf("%s already exists", p)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return "", err
		}
	}
	idx, err := r.index()
	if err != nil {
		return "", err
	}
	setList(idx, Kinds[kind], append(list(idx, Kinds[kind]), name))

	return filepath.Join(r.Dir, filepath.FromSlash(entryPath(kind, name))), r.saveIndex(idx)
}

// AddMCP scaffolds an MCP server entry: an http one at url, or a stdio one
// running cmd
func (r Repo) AddMCP(name, url, cmd string) (string, error) {
	m := MCP{Name: name, Description: "TODO: what it's for", Profiles: []string{"full", "minimal"}}
	switch {
	case url != "" && cmd != "":
		return "", errors.New("give --url or --cmd, not both")
	case url != "":
		m.Transport, m.URL = "http", url
	case cmd != "":
		f := strings.Fields(cmd)
		m.Transport, m.Command, m.Args = "stdio", f[0], f[1:]
	default:
		return "", errors.New("an MCP server needs --url or --cmd")
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}

	return r.add("mcp", name, map[string][]byte{entryPath("mcp", name): append(data, '\n')})
}

// AddPlugin lists a Claude Code plugin (name@marketplace) in the kit
func (r Repo) AddPlugin(id string) error {
	if !strings.Contains(id, "@") {
		return fmt.Errorf("give the plugin as name@marketplace, e.g. superpowers@claude-plugins-official")
	}
	_, err := r.add("plugin", id, nil)
	return err
}

// Scaffold makes a skill, agent or command entry to fill in
func (r Repo) Scaffold(kind, name string) (string, error) {
	var rel, body string
	switch kind {
	case "skill":
		rel = entryPath(kind, name) + "/SKILL.md"
		body = "---\nname: " + name + "\ndescription: TODO: when Claude should use this skill\n---\n\nTODO: the instructions.\n"
	case "agent":
		rel = entryPath(kind, name)
		body = "---\nname: " + name + "\ndescription: TODO: when to use this subagent\n---\n\nTODO: the subagent's system prompt.\n"
	case "command":
		rel = entryPath(kind, name)
		body = "---\ndescription: TODO: what /" + name + " does\n---\n\nTODO: the prompt. $ARGUMENTS is what follows the command.\n"
	case "output-style":
		rel = entryPath(kind, name)
		body = "---\nname: " + name + "\ndescription: TODO: what this style does\nkeep-coding-instructions: true\n---\n\nTODO: how replies should look.\n"
	default:
		return "", fmt.Errorf("can't scaffold a %s", kind)
	}
	p, err := r.add(kind, name, map[string][]byte{rel: []byte(body)})
	if kind == "skill" && err == nil {
		p = filepath.Join(p, "SKILL.md")
	}

	return p, err
}

// Remove takes an entry out of kit.json and deletes its files; the next sync
// removes it from Claude Code
func (r Repo) Remove(name string) (string, error) {
	kind, err := r.Find(name)
	if err != nil {
		return "", err
	}
	if kind == "" {
		return "", fmt.Errorf("the kit has nothing called %s", name)
	}
	idx, err := r.index()
	if err != nil {
		return "", err
	}
	setList(idx, Kinds[kind], slices.DeleteFunc(list(idx, Kinds[kind]), func(n string) bool { return n == name }))
	if err := r.saveIndex(idx); err != nil {
		return "", err
	}

	if kind == "plugin" {
		return kind, nil
	}

	return kind, os.RemoveAll(filepath.Join(r.Dir, filepath.FromSlash(entryPath(kind, name))))
}

// SetProfiles sets which profiles an MCP entry is on for
func (r Repo) SetProfiles(name string, profiles []string) error {
	p := filepath.Join(r.Dir, "mcp", name+".json")
	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("only MCP entries have profiles: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	list := make([]any, len(profiles))
	for i, pr := range profiles {
		list[i] = pr
	}
	m["profiles"] = list
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(p, append(out, '\n'), 0o644)
}

// Adopt pulls something you set up by hand in Claude Code into the kit: a
// user-scope MCP server from claudeJSON, or a skill, agent or command from
// claudeDir. An MCP server's header and env values become $secret:
// references, so no secret lands in the repo; secrets lists them, for you to
// store. kind is what it found
func (r Repo) Adopt(name, claudeDir, claudeJSON string) (kind string, secrets []string, err error) {
	if servers := userServers(claudeJSON); servers[name] != nil {
		var s struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Headers map[string]string `json:"headers"`
			Env     map[string]string `json:"env"`
		}
		if err := json.Unmarshal(servers[name], &s); err != nil {
			return "", nil, err
		}
		if s.Type == "" {
			s.Type = "stdio"
		}
		ref := func(in map[string]string) map[string]string {
			if len(in) == 0 {
				return nil
			}
			out := map[string]string{}
			for k := range in {
				secret := strings.ToUpper(regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(name+"_"+k, "_"))
				out[k] = secretPrefix + secret
				secrets = append(secrets, secret)
			}
			return out
		}
		m := MCP{
			Name: name, Description: "adopted from Claude Code", Transport: s.Type, URL: s.URL,
			Command: s.Command, Args: s.Args, Headers: ref(s.Headers), Env: ref(s.Env),
		}
		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return "", nil, err
		}
		slices.Sort(secrets)
		_, err = r.add("mcp", name, map[string][]byte{entryPath("mcp", name): append(data, '\n')})
		return "mcp", secrets, err
	}

	for _, kind := range []string{"skill", "agent", "command"} {
		src := filepath.Join(claudeDir, filepath.FromSlash(entryPath(kind, name)))
		info, err := os.Stat(src)
		if err != nil {
			continue
		}
		files := map[string][]byte{}
		if info.IsDir() {
			err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(src, p)
				data, err := os.ReadFile(p)
				files[entryPath(kind, name)+"/"+filepath.ToSlash(rel)] = data
				return err
			})
		} else {
			var data []byte
			data, err = os.ReadFile(src)
			files[entryPath(kind, name)] = data
		}
		if err != nil {
			return "", nil, err
		}
		_, err = r.add(kind, name, files)
		return kind, nil, err
	}

	return "", nil, fmt.Errorf("Claude Code has no MCP server, skill, agent or command called %s", name)
}
