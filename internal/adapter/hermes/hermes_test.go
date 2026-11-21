package hermes

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/adapter/env"
	"jgarner.dev/bud/internal/registry"
)

const testRegistry = `{
  "version": 1,
  "providers": {
    "go": {"api": "openai", "endpoint": "https://go.example/v1/", "key": "op://vault/go/key"},
    "qwen": {"api": "openai", "endpoint": "http://qwen.invalid:8080/v1", "local": true},
    "embed": {"api": "openai", "endpoint": "http://home.invalid:8082/v1", "local": true}
  },
  "models": {
    "qwen-35b": {"provider": "qwen", "name": "qwen-35b", "privacy": ["P0", "P1", "P2"], "cost": "C0", "context": 32768},
    "qwen-small": {"provider": "qwen", "name": "qwen-small", "privacy": ["P0", "P1", "P2"], "cost": "C0"},
    "fim": {"provider": "qwen", "name": "coder-fim", "privacy": ["P0", "P1", "P2"], "cost": "C0", "caps": ["fim"]},
    "glm": {"provider": "go", "name": "glm-5.3", "privacy": ["P1", "P2"], "cost": "C1", "context": 1000000},
    "emb": {"provider": "embed", "name": "emb", "privacy": ["P0"], "cost": "C0", "dims": 1024}
  },
  "roles": {
    "manager": {"pool": ["glm", "qwen-35b"]},
    "fallback": {"pool": ["qwen-small"]}
  }
}`

func testReg(t *testing.T, src string) *registry.Registry {
	t.Helper()
	r, err := registry.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func input(t *testing.T, srcs ...string) Input {
	t.Helper()
	in := Input{Registry: testReg(t, testRegistry), Gate: []string{"/home/you/.local/bin/bud", "hermes-gate", "/home/you/.local/state/bud/hermes-tiers.json"}}
	for _, s := range srcs {
		in.Env = append(in.Env, env.Fragment{Name: "test", Src: []byte(s)})
	}

	return in
}

func render(t *testing.T, srcs ...string) string {
	t.Helper()
	out, err := Render(input(t, srcs...))
	if err != nil {
		t.Fatal(err)
	}

	return string(out.Config)
}

func TestTheModelIsTheManagersFirstP0ModelAndFallbacksAreP0Only(t *testing.T) {
	out, err := Render(input(t))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out.Config)
	for _, want := range []string{
		"fallback_providers:\n  - model: \"qwen-small\"\n    provider: \"custom:qwen\"\n",
		"model:\n  default: \"qwen-35b\"\n  provider: \"custom:qwen\"\n",
		// only the chat models allowed P0 (D95)
		"providers:\n  qwen:\n    api: \"http://qwen.invalid:8080/v1\"\n    models:\n      qwen-35b:\n        context_length: 32768\n      qwen-small: {}\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks:\n%s\nin:\n%s", want, got)
		}
	}
	if strings.Contains(got, "embed:") || strings.Contains(got, "\"emb\"") || strings.Contains(got, "coder-fim") {
		t.Errorf("embedding and FIM models aren't chat models:\n%s", got)
	}
	if len(out.Notes) != 1 || !strings.Contains(out.Notes[0], "can't reach one: glm") {
		t.Errorf("notes: %q", out.Notes)
	}
}

func TestAManagerWithNoP0ModelIsRefused(t *testing.T) {
	in := input(t)
	in.Registry = testReg(t, strings.Replace(testRegistry, `"pool": ["qwen-small"]`, `"pool": ["glm"]`, 1))
	in.Registry.Roles["manager"] = registry.Role{Pool: []string{"glm"}}
	if _, err := Render(in); err == nil || !strings.Contains(err.Error(), "allowed P0") {
		t.Fatalf("got %v", err)
	}
}

func TestTheSafetySettingsAreRendered(t *testing.T) {
	in := input(t, `{"permissions": [
		{"action": "shell", "resource": "*", "effect": "ask"},
		{"action": "shell", "resource": "op *", "effect": "deny"},
		{"action": "read", "resource": "*.env", "effect": "ask"}
	]}`)
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out.Config)
	if !strings.HasPrefix(got, Header+"_config_version: 53\n") {
		t.Errorf("the config doesn't open with Hermes's schema version:\n%s", got)
	}
	for _, want := range []string{
		"approvals:\n  approvers: []\n  cron_mode: \"deny\"\n  deny:\n    - \"op *\"\n  mode: \"manual\"\n  single_query_mode: \"deny\"\n  timeout: 3600\n  unattended_mode: \"deny\"\n",
		"auxiliary:\n  background_review:\n    enabled: false\n",
		"command_allowlist: []\n",
		"curator:\n  consolidate: false\n",
		"hooks:\n  pre_tool_call:\n    - command: \"/home/you/.local/bin/bud hermes-gate /home/you/.local/state/bud/hermes-tiers.json\"\n      fail_closed: true\n      timeout: 10\n",
		"hooks_auto_accept: true\n",
		"memory:\n  write_approval: true\n",
		"skills:\n  external_dirs: []\n  write_approval: true\n",
		"terminal:\n  backend: \"docker\"\n  docker_mount_cwd_to_workspace: false\n",
		"updates:\n  check: false\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks:\n%s\nin:\n%s", want, got)
		}
	}
	var tiers Tiers
	if err := json.Unmarshal(out.Tiers, &tiers); err != nil {
		t.Fatal(err)
	}
	if len(tiers.Shell) != 2 || tiers.Shell[1].Resource != "op *" {
		t.Errorf("the tiers keep the shell rules only, in order: %+v", tiers.Shell)
	}
}

func TestAFragmentCantSetWhatBudOwns(t *testing.T) {
	for _, src := range []string{
		`{"bud": {"hermes": {"approvals": {"mode": "off"}}}}`,
		`{"bud": {"hermes": {"skills": {"write_approval": false}}}}`,
		`{"bud": {"hermes": {"terminal": {"backend": "local"}}}}`,
		`{"bud": {"hermes": {"model": {"default": "x"}}}}`,
		`{"bud": {"hermes": {"hooks": {}}}}`,
		`{"bud": {"hermes": {"_config_version": 1}}}`,
		// replacing a section bud shares with something that isn't a map
		// would replace bud's settings in it whole
		`{"bud": {"hermes": {"skills": false}}}`,
		`{"bud": {"hermes": {"memory": "off"}}}`,
		`{"bud": {"hermes": {"terminal": []}}}`,
		`{"bud": {"hermes": {"auxiliary": null}}}`,
		// no plugin's code runs but bud's, and Signal answers only Jason
		`{"bud": {"hermes": {"plugins": {"enabled": ["other"]}}}}`,
		`{"bud": {"hermes": {"platforms": {"signal": {"unauthorized_dm_behavior": "pair"}}}}}`,
		`{"bud": {"hermes": {"platforms": "off"}}}`,
	} {
		_, err := Render(input(t, src))
		if err == nil || !strings.Contains(err.Error(), "bud renders") {
			t.Errorf("%s: got %v", src, err)
		}
	}
	// the rest of a section it shares stays the fragment's
	got := render(t, `{"bud": {"hermes": {"terminal": {"docker_network": false}, "skills": {"inline_shell": false}}}}`)
	for _, want := range []string{"  docker_network: false\n", "  inline_shell: false\n", "  backend: \"docker\"\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
}

func TestSignalAndTheInstalledPluginsAreRendered(t *testing.T) {
	in := input(t)
	got := string(must(Render(in)).Config)
	for _, want := range []string{
		"platforms:\n  signal:\n    enabled: true\n    extra:\n      http_url: \"http://127.0.0.1:8793\"\n    unauthorized_dm_behavior: \"ignore\"\n",
		"plugins:\n  enabled: []\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks:\n%s\nin:\n%s", want, got)
		}
	}
	in.Plugins = []string{"bud-signal"}
	if got := string(must(Render(in)).Config); !strings.Contains(got, "plugins:\n  enabled:\n    - \"bud-signal\"\n") {
		t.Errorf("bud-signal isn't enabled:\n%s", got)
	}
	// another platform is the fragment's to set
	if got := render(t, `{"bud": {"hermes": {"platforms": {"discord": {"enabled": false}}}}}`); !strings.Contains(got, "  discord:\n    enabled: false\n  signal:\n") {
		t.Errorf("a fragment's other platform is lost:\n%s", got)
	}
}

func must(out *Output, err error) *Output {
	if err != nil {
		panic(err)
	}

	return out
}

func TestAFragmentsSecretsStayBesideTheProviderKeys(t *testing.T) {
	reg := strings.Replace(testRegistry, `"local": true},
    "embed"`, `"local": true, "key": "op://vault/qwen/key"},
    "embed"`, 1)
	in := input(t, `{"bud": {"hermes": {"secrets": {"onepassword": {"env": {"EXA_API_KEY": "op://vault/exa/key"}}}}}}`)
	in.Registry = testReg(t, reg)
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out.Config); !strings.Contains(got, "      BUD_QWEN_API_KEY: \"op://vault/qwen/key\"\n      EXA_API_KEY: \"op://vault/exa/key\"\n") {
		t.Errorf("got:\n%s", got)
	}
	in = input(t, `{"bud": {"hermes": {"secrets": {"onepassword": {"env": {"BUD_QWEN_API_KEY": "op://vault/other/key"}}}}}}`)
	in.Registry = testReg(t, reg)
	if _, err := Render(in); err == nil || !strings.Contains(err.Error(), "BUD_QWEN_API_KEY") {
		t.Errorf("a fragment that points the provider's key elsewhere: got %v", err)
	}
}

func TestHermesListsNoCloudModelAndLoadsNoCloudKey(t *testing.T) {
	out, err := Render(input(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out.Config)
	for _, bad := range []string{"glm-5.3", "go.example", "BUD_GO_API_KEY", "  go:\n"} {
		if strings.Contains(got, bad) {
			t.Errorf("a model not allowed P0 is reachable (%q):\n%s", bad, got)
		}
	}
	// the left-out provider's own key, and Hermes's built-in providers'
	// keys, which /model would use whatever the config lists
	for _, k := range []string{"BUD_GO_API_KEY", "OPENCODE_GO_API_KEY", "OPENROUTER_API_KEY"} {
		if !slices.Contains(out.CloudKeys, k) {
			t.Errorf("%s isn't a cloud key: %v", k, out.CloudKeys)
		}
	}
	// a key a listed (local) provider uses stays allowed
	reg := strings.Replace(testRegistry, `"local": true},
    "embed"`, `"local": true, "key": "{env:LM_API_KEY}"},
    "embed"`, 1)
	in := input(t, `{}`)
	in.Registry = testReg(t, reg)
	if out, err := Render(in); err != nil || slices.Contains(out.CloudKeys, "LM_API_KEY") {
		t.Errorf("a listed provider's key: err %v, cloud keys %v", err, out.CloudKeys)
	}
	if !slices.ContainsFunc(out.Notes, func(n string) bool { return strings.Contains(n, "/model can't reach one: glm") }) {
		t.Errorf("no note names the left-out model: %v", out.Notes)
	}
	// loading the left-out provider's key through a fragment is refused
	_, err = Render(input(t, `{"bud": {"hermes": {"secrets": {"onepassword": {"env": {"BUD_GO_API_KEY": "op://vault/go/key"}}}}}}`))
	if err == nil || !strings.Contains(err.Error(), "BUD_GO_API_KEY") || !strings.Contains(err.Error(), "D95") {
		t.Errorf("a fragment loading a cloud key: got %v", err)
	}
}

func TestServersAndSettingsRenderAsSortedYAML(t *testing.T) {
	got := render(t, `{"bud": {
		"privacy": "P1",
		"gateway": "http://127.0.0.1:8790",
		"mcp": {
			"linear": {"gateway": true, "privacy": ["P1"]},
			"github": {"command": ["github-mcp-server", "stdio"], "env": {"GITHUB_PERSONAL_ACCESS_TOKEN": "{env:GH_PAT}"}, "privacy": ["P1"]}
		},
		"hermes": {
			"web": {"keyless_fallback": false, "backend": "exa"},
			"platform_toolsets": {"cli": ["web", "file"]},
			"mcp_servers": {"linear": {"trust": "untrusted"}}
		}
	}}`)
	want := `mcp_servers:
  github:
    args:
      - "stdio"
    command: "github-mcp-server"
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GH_PAT}"
  linear:
    headers:
      X-Bud-Privacy: "P1"
    trust: "untrusted"
    url: "http://127.0.0.1:8790/linear/mcp"
`
	if !strings.Contains(got, want) {
		t.Fatalf("got:\n%s\nwant within it:\n%s", got, want)
	}
	if !strings.Contains(got, "platform_toolsets:\n  cli:\n    - \"web\"\n    - \"file\"\n") || !strings.Contains(got, "web:\n  backend: \"exa\"\n  keyless_fallback: false\n") {
		t.Fatalf("got:\n%s", got)
	}
}

// Suite D95: the cloud profile lists every chat model and its key, leaves
// out the servers only private sessions may use (with their extras), stays
// out of the private gateway process, and the tiers name the private tools
func TestTheCloudProfileHasCloudModelsAndNoPrivateServer(t *testing.T) {
	in := input(t, `{"bud": {
		"privacy": "P1",
		"gateway": "http://127.0.0.1:8790",
		"mcp": {
			"calendar": {"gateway": true, "privacy": ["P0"]},
			"linear": {"gateway": true, "privacy": ["P1", "P2"]}
		},
		"hermes": {"mcp_servers": {"calendar": {"trust": "untrusted"}, "linear": {"trust": "untrusted"}}}
	}}`)
	in.Cloud = true
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out.Config)
	for _, want := range []string{
		"  go:\n    api: \"https://go.example/v1\"\n    key_env: \"BUD_GO_API_KEY\"\n    models:\n      \"glm-5.3\":\n",
		"secrets:\n  onepassword:\n    enabled: true\n    env:\n      BUD_GO_API_KEY: \"op://vault/go/key\"\n",
		"model:\n  default: \"glm-5.3\"\n  provider: \"custom:go\"\n",
		"gateway:\n  standalone: true\n",
		"  linear:\n    headers:\n      X-Bud-Privacy: \"P1\"\n    trust: \"untrusted\"\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks:\n%s\nin:\n%s", want, got)
		}
	}
	if strings.Contains(got, "calendar") {
		t.Errorf("the cloud profile reaches the calendar:\n%s", got)
	}
	if !slices.ContainsFunc(out.Notes, func(n string) bool { return strings.Contains(n, "leaves out calendar") }) {
		t.Errorf("notes: %q", out.Notes)
	}
	var tiers Tiers
	if err := json.Unmarshal(out.Tiers, &tiers); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tiers.Private, []string{"mcp__calendar__*"}) {
		t.Errorf("private tools: %q", tiers.Private)
	}
	// a device whose own tier is P0, or unset, doesn't hand the cloud
	// profile a P0-only server (the gateway would read no header as P0)
	for _, tier := range []string{`"privacy": "P0",`, ``} {
		cin := input(t, `{"bud": {`+tier+` "gateway": "http://127.0.0.1:8790", "mcp": {"calendar": {"gateway": true, "privacy": ["P0"]}, "homeonly": {"command": ["x"], "privacy": ["P0"]}}}}`)
		cin.Cloud = true
		if c, err := Render(cin); err != nil || strings.Contains(string(c.Config), "calendar") || strings.Contains(string(c.Config), "homeonly") {
			t.Errorf("device tier %q: err %v\n%s", tier, err, c.Config)
		}
	}
	// the private profile stays in the gateway process that owns Signal
	in.Cloud = false
	if priv, err := Render(in); err != nil || strings.Contains(string(priv.Config), "standalone") {
		t.Errorf("the private profile: err %v\n%s", err, priv.Config)
	}
}

func TestAGatewayServerOnlyP0MayUseGetsAP0Header(t *testing.T) {
	got := render(t, `{"bud": {
		"privacy": "P1",
		"gateway": "http://127.0.0.1:8790",
		"mcp": {
			"calendar": {"gateway": true, "privacy": ["P0"]},
			"linear": {"gateway": true, "privacy": ["P1", "P2"]},
			"memory": {"gateway": true, "privacy": ["P0", "P1"]}
		}
	}}`)
	// Hermes runs only P0 models, so it may say P0 to a server the device's
	// tier can't use; the others keep the device's tier
	for _, want := range []string{
		"  calendar:\n    headers:\n      X-Bud-Privacy: \"P0\"\n",
		"  linear:\n    headers:\n      X-Bud-Privacy: \"P1\"\n",
		"  memory:\n    headers:\n      X-Bud-Privacy: \"P1\"\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	// with no device tier there is no header, which the gateway takes as P0
	got = render(t, `{"bud": {"gateway": "http://127.0.0.1:8790", "mcp": {"calendar": {"gateway": true, "privacy": ["P0"]}}}}`)
	if strings.Contains(got, "X-Bud-Privacy") {
		t.Errorf("a device with no tier got a header:\n%s", got)
	}
}

func TestAWordYAMLWouldReadAsABoolIsQuoted(t *testing.T) {
	got := render(t, `{"bud": {"hermes": {"x": {"on": 1, "no": "yes", "a b": [], "e": {}}}}}`)
	for _, want := range []string{`  "on": 1`, `  "no": "yes"`, `  "a b": []`, `  e: {}`} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
}

func TestAListOfMapsIsWrittenAndAListOfListsIsRefused(t *testing.T) {
	got := render(t, `{"bud": {"hermes": {"a": [{"b": 1, "c": {"d": true}}, {}]}}}`)
	if !strings.Contains(got, "a:\n  - b: 1\n    c:\n      d: true\n  - {}\n") {
		t.Errorf("got:\n%s", got)
	}
	_, err := Render(input(t, `{"bud": {"hermes": {"a": [[1]]}}}`))
	if err == nil || !strings.Contains(err.Error(), "a:") {
		t.Fatalf("got %v", err)
	}
}

func TestRenderingIsDeterministic(t *testing.T) {
	src := `{"bud": {"hermes": {"z": 1, "a": {"y": true, "b": "c"}}, "tiers": {"*": "ask", "read_file": "allow"}}}`
	a, err := Render(input(t, src))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(input(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if string(a.Config) != string(b.Config) || string(a.Tiers) != string(b.Tiers) {
		t.Fatal("two renders differ")
	}
}

func TestHermesExtrasMustNameADeclaredServerAndLeaveItsTransport(t *testing.T) {
	for src, want := range map[string]string{
		`{"bud": {"hermes": {"mcp_servers": {"rogue": {"url": "https://example.com/mcp"}}}}}`:                                                                  "rogue isn't in bud.mcp",
		`{"bud": {"mcp": {"c7": {"url": "https://example.com/mcp", "privacy": ["P1"]}}, "hermes": {"mcp_servers": {"c7": {"url": "https://x.example/mcp"}}}}}`: "c7.url comes from bud.mcp.c7",
	} {
		_, err := Render(input(t, src))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("got %v, want %q", err, want)
		}
	}
}

func TestShellJoinQuotesForShlex(t *testing.T) {
	got := shellJoin([]string{"/home/a b/bud", "hermes-gate", "it's.json"})
	if want := `'/home/a b/bud' hermes-gate 'it'"'"'s.json'`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestWithARouterThePrivateProfileAsksItForTheManager(t *testing.T) {
	in := input(t, `{"bud": {"router": "http://100.64.0.5:8795/"}}`)
	out, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out.Config)
	for _, want := range []string{
		"fallback_providers: []\n",
		"model:\n  default: \"bud/manager\"\n  provider: \"custom:bud\"\n",
		// one provider, the router, with no key and the smallest context the
		// router may pick (qwen-35b's, not glm's)
		"providers:\n  bud:\n    api: \"http://100.64.0.5:8795/v1\"\n    models:\n      \"bud/manager\":\n        context_length: 32768\n    transport: \"chat_completions\"\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks:\n%s\nin:\n%s", want, got)
		}
	}
	if strings.Contains(got, "qwen.invalid") || strings.Contains(got, "key_env") || strings.Contains(got, "X-Bud-Privacy: \"P1\"") {
		t.Errorf("the private profile reaches only the router, with no key and no tier header:\n%s", got)
	}
	if !slices.ContainsFunc(out.Notes, func(n string) bool {
		return strings.Contains(n, "asks the router at http://100.64.0.5:8795 for bud/manager")
	}) {
		t.Errorf("notes don't say Hermes goes through the router: %q", out.Notes)
	}

	in.Cloud = true
	cloud, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	if c := string(cloud.Config); strings.Contains(c, "bud/manager") || !strings.Contains(c, "glm-5.3") {
		t.Errorf("the cloud profile still names its providers itself:\n%s", c)
	}
}
