package curator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
)

// Occurrence is one approved record that states a fact
type Occurrence struct {
	Record  int64  `json:"record"`
	Source  string `json:"source"`
	Date    string `json:"date"`
	Privacy string `json:"privacy"`
}

// Candidate is one value for a fact key, with the records that state it
type Candidate struct {
	Value     string `json:"value"`
	Statement string `json:"statement"`
	// Page and Section are the titles the model gave, used when the curator
	// has to make the page or the section
	Page        string       `json:"page"`
	Section     string       `json:"section"`
	Occurrences []Occurrence `json:"occurrences"`
}

// State is what the curator has read and done. It lives outside the KB
// (paths.CuratorState), so when Jason reverts a curator commit the next
// run doesn't redo it from the same evidence (Suite D123)
type State struct {
	// Seen are records the model has read; Failed counts records it couldn't
	Seen   map[int64]bool `json:"seen"`
	Failed map[int64]int  `json:"failed,omitempty"`
	// Candidates are the values seen for each fact key
	Candidates map[string][]*Candidate `json:"candidates"`
	// Used is the evidence already acted on, by key and value: a candidate
	// acts again only once a record outside this list states it
	Used map[string][]int64 `json:"used"`
	// Promote are records Jason promoted by hand: one occurrence is enough
	Promote map[int64]bool `json:"promote,omitempty"`
}

func newState() *State {
	return &State{Seen: map[int64]bool{}, Failed: map[int64]int{}, Candidates: map[string][]*Candidate{}, Used: map[string][]int64{}, Promote: map[int64]bool{}}
}

// LoadState reads the state file; none yet is an empty state
func LoadState(path string) (*State, error) {
	s := newState()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("%s: %w (move it aside to start over; the curator will read every record again)", path, err)
	}
	if s.Seen == nil {
		s.Seen = map[int64]bool{}
	}
	if s.Failed == nil {
		s.Failed = map[int64]int{}
	}
	if s.Candidates == nil {
		s.Candidates = map[string][]*Candidate{}
	}
	if s.Used == nil {
		s.Used = map[string][]int64{}
	}
	if s.Promote == nil {
		s.Promote = map[int64]bool{}
	}

	return s, nil
}

// Save writes the state file
func (s *State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	return atomicfile.WriteFile(path, append(b, '\n'), 0o600)
}

func usedKey(key, value string) string { return key + "\x00" + value }

// fresh lists the candidate's records the curator hasn't acted on yet
func (s *State) fresh(key string, c *Candidate) []Occurrence {
	used := s.Used[usedKey(key, c.Value)]
	var out []Occurrence
	for _, o := range c.Occurrences {
		if !slices.Contains(used, o.Record) {
			out = append(out, o)
		}
	}

	return out
}

// use records that the candidate's evidence has been acted on
func (s *State) use(key, value string, occ []Occurrence) {
	k := usedKey(key, value)
	for _, o := range occ {
		if !slices.Contains(s.Used[k], o.Record) {
			s.Used[k] = append(s.Used[k], o.Record)
		}
	}
	slices.Sort(s.Used[k])
}

// add records that rec states value for key
func (s *State) add(key string, f Fact, o Occurrence) {
	for _, c := range s.Candidates[key] {
		if c.Value == f.Value {
			if !slices.ContainsFunc(c.Occurrences, func(x Occurrence) bool { return x.Record == o.Record }) {
				c.Occurrences = append(c.Occurrences, o)
			}
			c.Statement, c.Page, c.Section = f.Statement, f.Page, f.Section
			return
		}
	}
	s.Candidates[key] = append(s.Candidates[key], &Candidate{Value: f.Value, Statement: f.Statement, Page: f.Page, Section: f.Section, Occurrences: []Occurrence{o}})
}

// independent counts distinct (source, day) pairs, so one session saying
// the same thing twice counts once
func independent(occ []Occurrence) int {
	seen := map[string]bool{}
	for _, o := range occ {
		seen[o.Source+"\x00"+o.Date] = true
	}

	return len(seen)
}

// ParseRecordID reads a record id argument
func ParseRecordID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(s, "#"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%q isn't a memory record id: give the number, such as 12", s)
	}

	return id, nil
}
