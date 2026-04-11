package engine

import (
	"bytes"
)

// EnvMapFromBytes parses KEY=VALUE lines from an env file.
// Empty lines and lines starting with '#' are skipped.
// Lines without '=' are also skipped.
func EnvMapFromBytes(data []byte) map[string]string {
	m := make(map[string]string)
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		idx := bytes.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := string(bytes.TrimSpace(line[:idx]))
		value := string(bytes.TrimSpace(line[idx+1:]))
		m[key] = value
	}
	return m
}
