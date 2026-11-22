package router

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
)

var serverNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ReadP0Servers reads the list of P0-only MCP servers bud sync renders
// (paths.RouterP0Servers). A missing file is an error saying to sync, since
// without it the provenance rule (Suite D119) would raise nothing
func ReadP0Servers(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no list of the P0-only MCP servers at %s, so a calendar or mail result couldn't keep a chat local: run bud sync on this server device", path)
	}
	if err != nil {
		return nil, fmt.Errorf("read the P0-only servers: %w", err)
	}
	var f struct {
		Servers []string `json:"servers"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil || f.Servers == nil {
		return nil, fmt.Errorf("%s is damaged (%v): run bud sync", path, err)
	}
	for _, s := range f.Servers {
		if !serverNameRe.MatchString(s) {
			return nil, fmt.Errorf("%s: %q isn't a server name: run bud sync", path, s)
		}
	}

	return f.Servers, nil
}
