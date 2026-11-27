// Package envfile reads systemd-style env files and edits environment lists,
// for the commands that hand secrets to a child process
package envfile

import (
	"fmt"
	"os"
	"strings"
)

// Read reads a systemd-style env file: KEY=value lines, # comments, an
// optional export and quotes around the value
func Read(path string) ([][2]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out [][2]string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" || strings.HasPrefix(k, "#") {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out = append(out, [2]string{k, v})
	}

	return out, nil
}

// Value reads key's value from an env file; it's never printed. A file that
// doesn't set key, or sets it empty, is an error
func Value(path, key string) (string, error) {
	pairs, err := Read(path)
	if err != nil {
		return "", err
	}
	for _, kv := range pairs {
		if kv[0] == key && kv[1] != "" {
			return kv[1], nil
		}
	}

	return "", fmt.Errorf("%s doesn't set %s", path, key)
}

// Without drops key from an environment list
func Without(envv []string, key string) []string {
	var out []string
	for _, kv := range envv {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}

	return out
}

// With returns envv with key set to value, replacing any earlier setting
func With(envv []string, key, value string) []string {
	return append(Without(envv, key), key+"="+value)
}
