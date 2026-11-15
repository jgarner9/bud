package content

import (
	"encoding/json"
	"fmt"
)

// NewChannel is the channel.json the content release workflow publishes
// (Suite D60, D61): edge at the newest content release, stable at the newest
// one promoted, or nothing when none has been. The workflow builds it afresh
// from the release tags on every run, so a run that stopped halfway, or a
// rotated key, is put right by the next run rather than carried forward.
// Stable can never be ahead of edge
func NewChannel(edge, stable string) (*Channel, error) {
	e, err := ParseVersion(edge)
	if err != nil {
		return nil, fmt.Errorf("edge: %w", err)
	}
	c := &Channel{Channels: map[string]Versions{"edge": {Content: edge}}}
	if stable == "" {
		return c, nil
	}
	s, err := ParseVersion(stable)
	if err != nil {
		return nil, fmt.Errorf("stable: %w", err)
	}
	if s.Compare(e) > 0 {
		return nil, fmt.Errorf("stable %s is ahead of edge %s: stable only takes a version edge has offered", stable, edge)
	}
	c.Channels["stable"] = Versions{Content: stable}

	return c, nil
}

// MarshalChannel is channel.json's bytes, the ones that get signed
func MarshalChannel(c *Channel) ([]byte, error) {
	if c.Channels == nil {
		c.Channels = map[string]Versions{}
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(b, '\n'), nil
}
