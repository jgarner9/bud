// Package registry reads the provider and model registry (Suite D46, D58):
// one file, content/env/registry.jsonc, that names every provider, every
// model and which model fills each of the nine roles. bud sync renders each
// runtime's own config from it, so swapping a model for a role is one edit in
// one place. The private overlay's registry.jsonc is merged over the public
// one by ID, the overlay winning. The format is documented in docs/registry.md
package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Version is the registry format this bud reads
const Version = 1

// Roles are the nine roles a registry must fill (Suite D58). Routing and
// every runtime config point at a role, never at a model name
var Roles = []string{"manager", "coding", "planning", "classifier", "fim", "embedding", "fallback", "curator", "vision"}

// OptionalRoles may be filled, and nothing names them unless they are:
// quick answers short questions on a fast cloud model (Suite D121), since
// the local model's prefill makes even a short answer slow
var OptionalRoles = []string{"quick"}

// privateRoles see everything unclassified or personal, so every model they
// may use has to be allowed P0 data (Suite D23): the classifier reads a
// prompt before its tier is known, the curator writes personal KB pages, and
// embeddings cover all of working memory
var privateRoles = []string{"classifier", "curator", "embedding"}

// APIs are the wire formats a provider can speak
var APIs = []string{"openai"}

// Tiers are the privacy tiers a model may be given (Suite D23)
var Tiers = []string{"P0", "P1", "P2"}

// PrivacyHeader carries a request's privacy tier to the MCP gateway and the
// router
const PrivacyHeader = "X-Bud-Privacy"

// RaisedHeader names the privacy rule that raised a request's tier (Suite
// D112), so a client can say why a request stayed local; the router and the
// MCP gateway both send it
const RaisedHeader = "X-Bud-Privacy-Raised"

// RequestTier reads a PrivacyHeader value. A missing one is P0, so a client
// nobody configured fails closed (Suite D81); ok is false for any other
// value than a tier.
func RequestTier(v string) (tier string, ok bool) {
	if v == "" {
		return "P0", true
	}
	return v, slices.Contains(Tiers, v)
}

// Allows reports whether the model may see requests of the tier
func (m Model) Allows(tier string) bool { return slices.Contains(m.Privacy, tier) }

// Costs are the cost tiers (Suite D24)
var Costs = []string{"C0", "C1", "C2"}

// Inputs are what a model can be given (Suite D64)
var Inputs = []string{"text", "image", "video", "audio", "pdf"}

// Caps are what a model can do beyond plain chat (Suite D64). A provider's
// model list reports tools, reasoning and json; fim and embed are declared
// by hand
var Caps = []string{"tools", "reasoning", "json", "fim", "embed"}

// Grades say how hard a task a model is worth spending on: the router
// matches them to a request's difficulty (Suite D64)
var Grades = []string{"top", "mid", "light"}

type Registry struct {
	Version   int                 `json:"version"`
	Providers map[string]Provider `json:"providers"`
	Models    map[string]Model    `json:"models"`
	Roles     map[string]Role     `json:"roles"`
	// PrivacyRules raise a request's privacy tier (Suite D23, D112)
	PrivacyRules map[string]PrivacyRule `json:"privacy_rules,omitempty"`
}

// PrivacyRule raises a request's privacy tier when its pattern matches any
// one text of the request (Suite D23, D112). Rules only raise, never lower
type PrivacyRule struct {
	// Match is an RE2 regular expression (Go's regexp syntax), matched
	// against each message's text on its own; use (?i) for any case
	Match string `json:"match"`
	// Tier is P0 or P1: what a matching request becomes, unless it is
	// already more private
	Tier string `json:"tier"`
	// Entropy, when set, is the Shannon entropy in bits per character the
	// matched secret must be over, as in gitleaks' rules, so a placeholder
	// such as AKIAXXXXXXXXXXXXXXXX doesn't count (Suite D118)
	Entropy float64 `json:"entropy,omitempty"`
	// Validate, when set, names a check in Validators the matched text must
	// also pass: luhn (card numbers), iban, ssn or phone (Suite D118)
	Validate string `json:"validate,omitempty"`
	// Keywords, when set, are words one of which must be in the text, in
	// any case, before the pattern is tried, as in gitleaks' rules: a
	// substring check is far cheaper than a regular expression
	Keywords []string `json:"keywords,omitempty"`
}

// Provider is an endpoint models are served from
type Provider struct {
	API      string `json:"api"`
	Endpoint string `json:"endpoint"`
	// Key is how the runtime gets the API key: an op:// reference (Suite D7),
	// an {env:VAR} the runtime fills in, or empty for none. Never a value
	Key string `json:"key,omitempty"`
	// Local says the endpoint is inside the house, which is the only place P0
	// data may go (Suite D23)
	Local  bool   `json:"local,omitempty"`
	Region string `json:"region,omitempty"`
}

type Model struct {
	Provider string `json:"provider"`
	// Name is the model's id as its provider spells it
	Name    string   `json:"name"`
	Context int      `json:"context,omitempty"`
	Privacy []string `json:"privacy"`
	Cost    string   `json:"cost"`
	// Dims is an embedding model's vector size, which each stored vector
	// records so a swap is a re-embed job (Suite D52)
	Dims int `json:"dims,omitempty"`

	// The capability fields (Suite D64). Inputs, Caps, Context, Output and
	// Price are facts bud registry refresh reads from the provider; Grade,
	// Allowance, Retention and Trains are kept by hand
	Inputs []string `json:"inputs,omitempty"`
	Caps   []string `json:"caps,omitempty"`
	Output int      `json:"output,omitempty"`
	Grade  string   `json:"grade,omitempty"`
	// Allowance is the plan's included spend on this model, in dollars a month
	Allowance float64 `json:"allowance,omitempty"`
	Price     *Price  `json:"price,omitempty"`
	// Retention is how many days the provider keeps requests; nil is unknown.
	// Only a model kept 0 days may see P1 (Suite D65)
	Retention *int `json:"retention,omitempty"`
	// Trains says the provider trains on request data, so the model may see
	// only P2 (Suite D65)
	Trains bool `json:"trains,omitempty"`
}

// Price is dollars per million tokens
type Price struct {
	In  float64 `json:"in"`
	Out float64 `json:"out"`
}

// Has says whether the model has a capability or takes an input
func (m Model) Has(need string) bool {
	return slices.Contains(m.Caps, need) || slices.Contains(m.Inputs, need)
}

// Role lists the models a role may use. Pool is the current form (Suite
// D64): the router picks among its members, in preference order. Model plus
// Fallbacks is the older form and reads as a pool of [Model, Fallbacks...]
type Role struct {
	Model     string   `json:"model,omitempty"`
	Fallbacks []string `json:"fallbacks,omitempty"`
	Pool      []string `json:"pool,omitempty"`
	// Needs are capabilities or inputs every member must have
	Needs []string `json:"needs,omitempty"`
	// Pin, when set, is the member the router always uses
	Pin string `json:"pin,omitempty"`
}

// Members returns the role's models in preference order
func (r Role) Members() []string {
	if len(r.Pool) > 0 {
		return r.Pool
	}
	if r.Model == "" {
		return nil
	}

	return append([]string{r.Model}, r.Fallbacks...)
}

// Default is the model a runtime is configured with when it takes one model
// per role: the pin, or else the first member
func (r Role) Default() string {
	if r.Pin != "" {
		return r.Pin
	}
	if m := r.Members(); len(m) > 0 {
		return m[0]
	}

	return ""
}

// Load reads a registry file and validates it
func Load(path string) (*Registry, error) {
	r, err := Read(path)
	if err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return r, nil
}

// Read parses a registry file without validating it, for a part that is
// merged before it is checked, like the overlay's
func Read(path string) (*Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the registry: %w", err)
	}
	r, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return r, nil
}

// Parse decodes a registry from JSONC. A field it doesn't know is an error,
// so a typo can't quietly drop a setting
func Parse(src []byte) (*Registry, error) {
	b, err := StripJSONC(src)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var r Registry
	if err := dec.Decode(&r); err != nil {
		return nil, decodeError(b, dec, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("there is more after the registry's closing }")
	}

	return &r, nil
}

// decodeError gives a JSON error the line it happened on
func decodeError(b []byte, dec *json.Decoder, err error) error {
	off := dec.InputOffset()
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		off = syntax.Offset
	}
	var typ *json.UnmarshalTypeError
	if errors.As(err, &typ) {
		off = typ.Offset
	}
	off = min(max(off, 0), int64(len(b)))
	// an unknown field is reported after its value is read, so point at the
	// key itself: the last time it appears before that
	if name, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		if i := bytes.LastIndex(b[:off], []byte(name)); i >= 0 {
			off = int64(i)
		}
	}

	return fmt.Errorf("line %d: %w", bytes.Count(b[:off], []byte("\n"))+1, err)
}

// Merge returns base with overlay on top: an overlay provider, model, role or
// privacy rule replaces the base entry of the same ID whole, and new IDs are added
func Merge(base, overlay *Registry) (*Registry, error) {
	if overlay == nil {
		return base, nil
	}
	if overlay.Version != 0 && overlay.Version != base.Version {
		return nil, fmt.Errorf("the overlay registry is version %d but the public one is %d; bring the overlay's version in line", overlay.Version, base.Version)
	}
	out := &Registry{
		Version:      base.Version,
		Providers:    merged(base.Providers, overlay.Providers),
		Models:       merged(base.Models, overlay.Models),
		Roles:        merged(base.Roles, overlay.Roles),
		PrivacyRules: merged(base.PrivacyRules, overlay.PrivacyRules),
	}

	return out, nil
}

func merged[V any](base, top map[string]V) map[string]V {
	out := make(map[string]V, len(base)+len(top))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range top {
		out[k] = v
	}

	return out
}

var (
	idRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	envRe = regexp.MustCompile(`^\{env:[A-Z_][A-Z0-9_]*\}$`)
	// 1Password names may hold single spaces ("op://example vault/API Key/credential"),
	// which op run reads unquoted; no segment starts or ends with one
	opRefRe  = regexp.MustCompile("^op:" + "//" + `[^/\s]+( [^/\s]+)*/[^/\s]+( [^/\s]+)*(/[^/\s]+( [^/\s]+)*)+$`)
	errorSep = "\n  "
)

// IsOpRef says whether s is a whole op://vault/item/field reference
func IsOpRef(s string) bool { return opRefRe.MatchString(s) }

// Validate checks that the registry is complete and consistent. It reports
// every problem at once, sorted, each saying what to fix
func (r *Registry) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if r.Version != Version {
		add("version: is %d, but this bud reads version %d; set \"version\": %d", r.Version, Version, Version)
	}
	if len(r.Providers) == 0 {
		add("providers: none listed; add at least one")
	}
	for id, p := range r.Providers {
		at := "providers." + id
		if !idRe.MatchString(id) {
			add("%s: an ID is lowercase letters, digits, '.', '_' and '-'", at)
		}
		if !slices.Contains(APIs, p.API) {
			add("%s.api: %q isn't one of %s", at, p.API, strings.Join(APIs, ", "))
		}
		if u, err := url.Parse(p.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			add("%s.endpoint: %q isn't an http or https URL", at, p.Endpoint)
		}
		if p.Key != "" && !envRe.MatchString(p.Key) && !opRefRe.MatchString(p.Key) {
			add("%s.key: must be an op://vault/item/field reference or {env:VAR}, never a key itself", at)
		}
	}

	// Generated provider key names normalize punctuation to underscores.
	// Refuse two different sources for the same environment variable.
	keySources := map[string]string{}
	for id, provider := range r.Providers {
		if provider.Key == "" {
			continue
		}
		name := ""
		if strings.HasPrefix(provider.Key, "{env:") && strings.HasSuffix(provider.Key, "}") {
			name = strings.TrimSuffix(strings.TrimPrefix(provider.Key, "{env:"), "}")
		} else if strings.HasPrefix(provider.Key, "op://") {
			normalized := strings.Map(func(r rune) rune {
				if r >= 'a' && r <= 'z' {
					return r - 'a' + 'A'
				}
				if r >= '0' && r <= '9' {
					return r
				}
				return '_'
			}, id)
			name = "BUD_" + normalized + "_API_KEY"
		}
		if name == "" {
			continue
		}
		if previous, ok := keySources[name]; ok && previous != provider.Key {
			add("providers: conflicting key sources for %s; rename a provider or use distinct environment variables", name)
		} else {
			keySources[name] = provider.Key
		}
	}

	if len(r.Models) == 0 {
		add("models: none listed; add at least one")
	}
	for id, m := range r.Models {
		at := "models." + id
		if !idRe.MatchString(id) {
			add("%s: an ID is lowercase letters, digits, '.', '_' and '-'", at)
		}
		p, ok := r.Providers[m.Provider]
		if !ok {
			add("%s.provider: %q isn't in providers; add it there or name one that is", at, m.Provider)
		}
		if m.Name == "" {
			add("%s.name: empty; give the model's id as its provider spells it", at)
		}
		if m.Context < 0 || m.Dims < 0 {
			add("%s: context and dims can't be negative", at)
		}
		if len(m.Privacy) == 0 {
			add("%s.privacy: empty; list the tiers it may see, from %s", at, strings.Join(Tiers, ", "))
		}
		for _, t := range m.Privacy {
			if !slices.Contains(Tiers, t) {
				add("%s.privacy: %q isn't one of %s", at, t, strings.Join(Tiers, ", "))
			}
		}
		if slices.Contains(m.Privacy, "P0") && ok && !p.Local {
			add("%s.privacy: allows P0, but provider %q isn't local; P0 never leaves the house (Suite D23)", at, m.Provider)
		}
		if !slices.Contains(Costs, m.Cost) {
			add("%s.cost: %q isn't one of %s", at, m.Cost, strings.Join(Costs, ", "))
		}
		checkModelCaps(at, m, add)
	}

	for _, role := range Roles {
		if _, ok := r.Roles[role]; !ok {
			add("roles.%s: missing; every one of %s needs a model", role, strings.Join(Roles, ", "))
		}
	}
	for name, role := range r.Roles {
		at := "roles." + name
		if !slices.Contains(Roles, name) && !slices.Contains(OptionalRoles, name) {
			add("%s: not a role; the roles are %s, and optionally %s", at, strings.Join(Roles, ", "), strings.Join(OptionalRoles, ", "))
			continue
		}
		checkRole(r, name, role, add)
	}

	for name, rule := range r.PrivacyRules {
		checkPrivacyRule(name, rule, add)
	}

	if len(errs) == 0 {
		return nil
	}
	sort.Strings(errs)

	return fmt.Errorf("%d %s:%s%s", len(errs), plural(len(errs), "problem"), errorSep, strings.Join(errs, errorSep))
}

// checkPrivacyRule checks one privacy rule's name, pattern and tier
func checkPrivacyRule(name string, rule PrivacyRule, add func(string, ...any)) {
	at := "privacy_rules." + name
	if !idRe.MatchString(name) {
		add("%s: an ID is lowercase letters, digits, '.', '_' and '-'", at)
	}
	if rule.Match == "" {
		add("%s: match is empty; give it a regular expression", at)
	} else if _, err := regexp.Compile(rule.Match); err != nil {
		add("%s: match doesn't compile: %v; it is Go's RE2 syntax", at, err)
	}
	if rule.Entropy < 0 || rule.Entropy > 8 {
		add("%s: entropy is %v; it is bits per character, between 0 and 8", at, rule.Entropy)
	}
	for _, k := range rule.Keywords {
		if k == "" || k != strings.ToLower(k) {
			add("%s: keyword %q must be lowercase and not empty; they match in any case", at, k)
		}
	}
	if _, ok := Validators[rule.Validate]; rule.Validate != "" && !ok {
		add("%s: validate is %q; use luhn, iban, ssn or phone", at, rule.Validate)
	}
	switch rule.Tier {
	case "P0", "P1":
	case "P2":
		add("%s: tier P2 can't raise anything, since P2 is the least private; use P0 or P1", at)
	default:
		add("%s: tier is %q; use P0 or P1", at, rule.Tier)
	}
}

// checkModelCaps checks the capability fields (Suite D64, D65)
func checkModelCaps(at string, m Model, add func(string, ...any)) {
	for _, in := range m.Inputs {
		if !slices.Contains(Inputs, in) {
			add("%s.inputs: %q isn't one of %s", at, in, strings.Join(Inputs, ", "))
		}
	}
	for _, c := range m.Caps {
		if !slices.Contains(Caps, c) {
			add("%s.caps: %q isn't one of %s", at, c, strings.Join(Caps, ", "))
		}
	}
	if m.Grade != "" && !slices.Contains(Grades, m.Grade) {
		add("%s.grade: %q isn't one of %s", at, m.Grade, strings.Join(Grades, ", "))
	}
	if m.Output < 0 || m.Allowance < 0 || (m.Price != nil && (m.Price.In < 0 || m.Price.Out < 0)) || (m.Retention != nil && *m.Retention < 0) {
		add("%s: output, allowance, price and retention can't be negative", at)
	}
	if m.Trains && slices.ContainsFunc(m.Privacy, func(t string) bool { return t != "P2" }) {
		add("%s.privacy: the provider trains on its requests, so it may see only P2 (Suite D65)", at)
	} else if m.Retention != nil && *m.Retention > 0 && slices.ContainsFunc(m.Privacy, func(t string) bool { return t != "P2" }) {
		add("%s.privacy: the provider keeps requests %d days, so it may see only P2; only a 0-day model sees P1 (Suite D65)", at, *m.Retention)
	}
}

// checkRole checks one role's members, needs and pin
func checkRole(r *Registry, name string, role Role, add func(string, ...any)) {
	at := "roles." + name
	if len(role.Pool) > 0 && (role.Model != "" || len(role.Fallbacks) > 0) {
		add("%s: has both a pool and model/fallbacks; keep the pool", at)
		return
	}
	if len(role.Members()) == 0 {
		add("%s: lists no models; give it a pool", at)
		return
	}
	for _, need := range role.Needs {
		if !slices.Contains(Caps, need) && !slices.Contains(Inputs, need) {
			add("%s.needs: %q isn't a capability (%s) or an input (%s)", at, need, strings.Join(Caps, ", "), strings.Join(Inputs, ", "))
		}
	}
	seen := map[string]bool{}
	for i, id := range role.Members() {
		field := fmt.Sprintf("%s.pool[%d]", at, i)
		if len(role.Pool) == 0 {
			field = at + ".model"
			if i > 0 {
				field = fmt.Sprintf("%s.fallbacks[%d]", at, i-1)
			}
		}
		if seen[id] {
			add("%s: %q is listed twice; drop the repeat", field, id)
			continue
		}
		seen[id] = true
		m, ok := r.Models[id]
		if !ok {
			add("%s: %q isn't in models; add it there or name one that is", field, id)
			continue
		}
		if slices.Contains(privateRoles, name) && !slices.Contains(m.Privacy, "P0") {
			add("%s: %q isn't allowed P0, but %s sees private data; use a local model", field, id, name)
		}
		if name == "embedding" && m.Dims == 0 {
			add("%s: %q has no dims; embedding models record their vector size", field, id)
		}
		for _, need := range role.Needs {
			if !m.Has(need) {
				add("%s: %q lacks %q, which %s needs; add it to the model's caps or inputs if it has it, or drop it from the pool", field, id, need, name)
			}
		}
	}
	if role.Pin != "" && !seen[role.Pin] {
		add("%s.pin: %q isn't in the role's pool; add it there or pin a member", at, role.Pin)
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}

	return word + "s"
}

// KeyVar is the environment variable a provider's key comes from: the one
// an {env:VAR} names, or BUD_<PROVIDER>_API_KEY for an op:// reference. It
// is empty for a provider with no key. The OpenCode adapter and the router
// both use it, so a unit's env file feeds either
func KeyVar(provider, key string) string {
	switch {
	case key == "":
		return ""
	case strings.HasPrefix(key, "{env:"):
		return strings.TrimSuffix(strings.TrimPrefix(key, "{env:"), "}")
	}
	up := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			return r
		}
		return '_'
	}, provider)
	return "BUD_" + up + "_API_KEY"
}
