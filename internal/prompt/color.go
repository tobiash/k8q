// Package prompt provides terminal-aware output formatting for k8q,
// including ANSI colorization of YAML output.
package prompt

import (
	"bytes"
	"io"
	"os"
	"strings"
)

// ANSI color codes.
const (
	reset   = "\033[0m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	gray    = "\033[90m"
	bold    = "\033[1m"
)

// IsTerminal returns true if the writer wraps a file that is a terminal.
func IsTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil {
			return false
		}
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// NoColorEnv returns true if the NO_COLOR environment variable is set
// (per https://no-color.org/).
func NoColorEnv() bool {
	_, set := os.LookupEnv("NO_COLOR")
	return set
}

// ColorizeYAML applies ANSI colors to a YAML string. It tokenizes the
// output at a line level and applies colors based on structural cues:
//   - Keys (lines ending with ":") — bold cyan
//   - String values (quoted or plain after ": ") — green
//   - Numeric values — yellow
//   - Boolean values (true/false) — magenta
//   - Null values — gray
//   - Document separator "---" — gray
//   - List markers "- " — bold
func ColorizeYAML(in string) string {
	var buf strings.Builder
	lines := strings.Split(in, "\n")

	for i, line := range lines {
		buf.WriteString(colorizeLine(line))
		if i < len(lines)-1 {
			buf.WriteByte('\n')
		}
	}

	return buf.String()
}

func colorizeLine(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	indent := line[:len(line)-len(trimmed)]

	if trimmed == "" {
		return line
	}

	// Document separator.
	if trimmed == "---" || strings.HasPrefix(trimmed, "--- ") {
		return gray + line + reset
	}

	// List item with a value: "  - key: val" or "  - value"
	if strings.HasPrefix(trimmed, "- ") {
		rest := trimmed[2:]
		return indent + bold + "- " + reset + colorizeLine(rest)
	}

	// Key-value line.
	if idx := strings.Index(trimmed, ": "); idx >= 0 {
		key := trimmed[:idx]
		val := trimmed[idx+2:]
		return indent + bold + cyan + key + reset + ": " + colorizeValue(val)
	}

	// Key-only line (mapping key with value on next line).
	if strings.HasSuffix(trimmed, ":") {
		key := trimmed[:len(trimmed)-1]
		return indent + bold + cyan + key + reset + ":"
	}

	// Bare value (list item continued).
	return indent + colorizeValue(trimmed)
}

func colorizeValue(val string) string {
	if val == "" {
		return ""
	}

	// Quoted string.
	if (strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`)) ||
		(strings.HasPrefix(val, `'`) && strings.HasSuffix(val, `'`)) {
		return green + val + reset
	}

	// Boolean.
	if val == "true" || val == "false" {
		return magenta + val + reset
	}

	// Null.
	if val == "null" || val == "~" {
		return gray + val + reset
	}

	// Numeric (integer or float).
	if isNumeric(val) {
		return yellow + val + reset
	}

	// Plain string.
	return green + val + reset
}

func isNumeric(s string) bool {
	if s == "" || s == "-" || s == "+" {
		return false
	}
	// Allow leading sign.
	i := 0
	if s[0] == '-' || s[0] == '+' {
		i = 1
	}
	if i >= len(s) {
		return false
	}
	hasDot := false
	hasE := false
	for ; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '.' && !hasDot && !hasE {
			hasDot = true
			continue
		}
		if (c == 'e' || c == 'E') && !hasE {
			hasE = true
			continue
		}
		if (c == '+' || c == '-') && hasE && (s[i-1] == 'e' || s[i-1] == 'E') {
			continue
		}
		return false
	}
	return true
}

// ColorWriter wraps an io.Writer to colorize YAML output before writing.
// If color is disabled, writes pass through unchanged.
type ColorWriter struct {
	dst   io.Writer
	buf   bytes.Buffer
	color bool
}

// NewColorWriter creates a ColorWriter that writes to dst. If color is true,
// YAML output is colorized; otherwise it passes through unchanged.
func NewColorWriter(dst io.Writer, color bool) *ColorWriter {
	return &ColorWriter{dst: dst, color: color}
}

func (cw *ColorWriter) Write(p []byte) (int, error) {
	if !cw.color {
		return cw.dst.Write(p)
	}
	cw.buf.Write(p)
	return len(p), nil
}

// Flush colorizes buffered content and writes it to the underlying writer.
func (cw *ColorWriter) Flush() error {
	if !cw.color {
		return nil
	}
	data := cw.buf.String()
	cw.buf.Reset()
	colorized := ColorizeYAML(data)
	_, err := cw.dst.Write([]byte(colorized))
	return err
}

// Close flushes remaining buffered content.
func (cw *ColorWriter) Close() error {
	return cw.Flush()
}
