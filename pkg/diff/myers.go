package diff

import (
	"fmt"
	"io"
	"strings"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
)

// Re-export gotextdiff types so consumers don't need to import it directly.
type (
	Unified = gotextdiff.Unified
	Hunk    = gotextdiff.Hunk
	Line    = gotextdiff.Line
	OpKind  = gotextdiff.OpKind
)

const (
	Delete = gotextdiff.Delete
	Insert = gotextdiff.Insert
	Equal  = gotextdiff.Equal
)

// ComputeDiff computes a Myers diff between two multi-line strings and returns
// a Unified diff structure.
func ComputeDiff(name, before, after string) Unified {
	edits := myers.ComputeEdits(span.URIFromPath(name), before, after)
	return gotextdiff.ToUnified(name, name, before, edits)
}

// Format writes a unified diff to w.
func Format(w io.Writer, u Unified) {
	fmt.Fprint(w, u)
}

// FormatString renders a unified diff as a string.
func FormatString(u Unified) string {
	var sb strings.Builder
	Format(&sb, u)
	return sb.String()
}
