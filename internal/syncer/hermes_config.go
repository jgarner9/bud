package syncer

import (
	"path/filepath"
	"strings"
)

// Older ledgers record the two Hermes configs as ordinary rendered configs.
func isHermesConfig(f File) bool {
	return f.Kind == "config" && f.Layer == "rendered" && filepath.Base(f.Path) == "config.yaml"
}

type hermesState struct {
	onboarding    string
	serviceChoice string
}

// splitHermesConfig handles the block layout that Bud emits and Hermes
// updates in place. Keep all other bytes in the ownership comparison.
func splitHermesConfig(data []byte) ([]byte, hermesState) {
	var managed strings.Builder
	var state hermesState
	for rest := string(data); rest != ""; {
		line, tail := yamlLine(rest)
		switch line {
		case "onboarding:\n":
			block, after := yamlChildren(tail)
			if block != "" && state.onboarding == "" {
				state.onboarding = line + block
				rest = after
				continue
			}
		case "gateway:\n":
			block, after := yamlChildren(tail)
			if state.serviceChoice == "" {
				for child := block; child != ""; {
					field, next := yamlLine(child)
					if strings.HasPrefix(field, "  service_install_choice: ") {
						switch strings.TrimSpace(strings.TrimPrefix(field, "  service_install_choice: ")) {
						case "install", "decline", "null", "~", "'install'", "'decline'", `"install"`, `"decline"`:
							state.serviceChoice = field
							block = strings.TrimSuffix(block, child) + next
						}
						break
					}
					child = next
				}
				if state.serviceChoice != "" {
					if block != "" {
						managed.WriteString(line + block)
					}
					rest = after
					continue
				}
			}
		}
		managed.WriteString(line)
		rest = tail
	}
	return []byte(managed.String()), state
}

func yamlLine(data string) (string, string) {
	if n := strings.IndexByte(data, '\n'); n >= 0 {
		return data[:n+1], data[n+1:]
	}
	return data, ""
}

func yamlChildren(data string) (string, string) {
	rest := data
	for strings.HasPrefix(rest, " ") {
		line, next := yamlLine(rest)
		if strings.TrimSpace(line) == "" {
			break
		}
		rest = next
	}
	return data[:len(data)-len(rest)], rest
}

func (s hermesState) carryInto(data []byte) []byte {
	if s.serviceChoice != "" {
		choice := strings.TrimSuffix(s.serviceChoice, "\n") + "\n"
		cfg := string(data)
		const gateway = "\ngateway:"
		switch start := strings.Index(cfg, gateway); {
		case start < 0:
			data = append(data, "gateway:\n"+choice...)
		case strings.HasPrefix(cfg[start+len(gateway):], "\n"):
			start += len(gateway) + 1
			children, _ := yamlChildren(cfg[start:])
			if !strings.Contains("\n"+children, "\n  service_install_choice:") {
				data = []byte(cfg[:start] + choice + cfg[start:])
			}
		}
	}
	if s.onboarding != "" && !strings.Contains(string(data), "\nonboarding:") {
		data = append(data, s.onboarding...)
		if data[len(data)-1] != '\n' {
			data = append(data, '\n')
		}
	}
	return data
}
