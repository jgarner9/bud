package content

import (
	"encoding/json"
	"fmt"
	"slices"

	"jgarner.dev/bud/internal/profile"
)

// SetContent moves channel's content version to version, the way the
// content release workflow does (Suite D60, D61): edge to each new release,
// stable to a version edge has already had. A channel only moves forward,
// so a re-signed channel.json can never roll devices back. It reports
// whether anything changed; naming the version a channel is already at
// changes nothing
func SetContent(c *Channel, channel, version string) (bool, error) {
	if !slices.Contains(profile.Channels, channel) {
		return false, fmt.Errorf("%q isn't a channel (%v)", channel, profile.Channels)
	}
	if _, err := ParseVersion(version); err != nil {
		return false, err
	}
	cur := c.Channels[channel]
	if cur.Content == version {
		return false, nil
	}
	if err := CheckNewer(cur.Content, version); err != nil {
		return false, fmt.Errorf("refusing to move %s from %s to %s: a channel only moves forward", channel, cur.Content, version)
	}
	if channel == "stable" {
		edge := c.Channels["edge"].Content
		if edge == "" || CheckNewer(edge, version) == nil {
			return false, fmt.Errorf("refusing to promote %s to stable: edge is at %q, and stable only takes a version edge has had", version, edge)
		}
	}
	if c.Channels == nil {
		c.Channels = map[string]Versions{}
	}
	cur.Content = version
	c.Channels[channel] = cur

	return true, nil
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
