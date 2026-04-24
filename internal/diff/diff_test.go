package diff

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestComputeOps_Identical(t *testing.T) {
	a := []string{"foo\n", "bar\n", "baz\n"}
	ops := ComputeOps(a, a)
	if len(ops) != 0 {
		t.Fatalf("expected no ops for identical input, got %d", len(ops))
	}
}

func TestComputeOps_Empty(t *testing.T) {
	ops := ComputeOps(nil, nil)
	if len(ops) != 0 {
		t.Fatalf("expected no ops for empty input, got %d", len(ops))
	}
}

func TestComputeOps_AllInserted(t *testing.T) {
	b := []string{"a\n", "b\n", "c\n"}
	ops := ComputeOps(nil, b)
	if len(ops) == 0 {
		t.Fatal("expected insert ops")
	}
	for _, op := range ops {
		if op.Kind != Insert {
			t.Fatalf("expected all inserts, got %v", op.Kind)
		}
	}
}

func TestComputeOps_AllDeleted(t *testing.T) {
	a := []string{"a\n", "b\n", "c\n"}
	ops := ComputeOps(a, nil)
	if len(ops) == 0 {
		t.Fatal("expected delete ops")
	}
	for _, op := range ops {
		if op.Kind != Delete {
			t.Fatalf("expected all deletes, got %v", op.Kind)
		}
	}
}

func TestComputeOps_SingleChange(t *testing.T) {
	a := []string{"foo\n", "bar\n", "baz\n"}
	b := []string{"foo\n", "qux\n", "baz\n"}
	ops := ComputeOps(a, b)

	var deletes, inserts int
	for _, op := range ops {
		switch op.Kind {
		case Delete:
			deletes++
		case Insert:
			inserts++
		}
	}
	if deletes != 1 || inserts != 1 {
		t.Fatalf("expected 1 delete + 1 insert, got %d deletes + %d inserts", deletes, inserts)
	}
}

func TestToUnified_NoChanges(t *testing.T) {
	a := []string{"foo\n", "bar\n"}
	ops := ComputeOps(a, a)
	u := ToUnified("a.txt", "b.txt", a, a, ops)
	if len(u.Hunks) != 0 {
		t.Fatalf("expected no hunks for identical input, got %d", len(u.Hunks))
	}
}

func TestToUnified_SingleChange(t *testing.T) {
	a := []string{"foo\n", "bar\n", "baz\n"}
	b := []string{"foo\n", "qux\n", "baz\n"}
	ops := ComputeOps(a, b)
	u := ToUnified("a.txt", "b.txt", a, b, ops)

	if len(u.Hunks) == 0 {
		t.Fatal("expected at least one hunk")
	}

	h := u.Hunks[0]
	var hasDelete, hasInsert bool
	for _, l := range h.Lines {
		switch l.Kind {
		case Delete:
			hasDelete = true
			if l.Content != "bar\n" {
				t.Fatalf("expected deleted line 'bar\\n', got %q", l.Content)
			}
		case Insert:
			hasInsert = true
			if l.Content != "qux\n" {
				t.Fatalf("expected inserted line 'qux\\n', got %q", l.Content)
			}
		}
	}
	if !hasDelete || !hasInsert {
		t.Fatal("expected both delete and insert lines in hunk")
	}
}

func TestFormat_HunkHeaders(t *testing.T) {
	a := []string{"line1\n", "line2\n", "line3\n", "line4\n", "line5\n", "line6\n", "line7\n"}
	b := []string{"line1\n", "line2\n", "line3\n", "changed\n", "line5\n", "line6\n", "line7\n"}
	ops := ComputeOps(a, b)
	u := ToUnified("before.yaml", "after.yaml", a, b, ops)
	out := FormatString(u)

	if !strings.Contains(out, "--- before.yaml\n") {
		t.Fatalf("expected --- header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "+++ after.yaml\n") {
		t.Fatalf("expected +++ header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "@@") {
		t.Fatalf("expected @@ hunk header in output, got:\n%s", out)
	}
}

func TestFormat_AddedResources(t *testing.T) {
	a := []string{"line1\n"}
	b := []string{"line1\n", "line2\n", "line3\n"}
	ops := ComputeOps(a, b)
	u := ToUnified("a", "b", a, b, ops)
	out := FormatString(u)

	if !strings.Contains(out, "+line2\n") {
		t.Fatalf("expected +line2 in output, got:\n%s", out)
	}
	if !strings.Contains(out, "+line3\n") {
		t.Fatalf("expected +line3 in output, got:\n%s", out)
	}
}

func TestFormat_RemovedResources(t *testing.T) {
	a := []string{"line1\n", "line2\n", "line3\n"}
	b := []string{"line1\n"}
	ops := ComputeOps(a, b)
	u := ToUnified("a", "b", a, b, ops)
	out := FormatString(u)

	if !strings.Contains(out, "-line2\n") {
		t.Fatalf("expected -line2 in output, got:\n%s", out)
	}
	if !strings.Contains(out, "-line3\n") {
		t.Fatalf("expected -line3 in output, got:\n%s", out)
	}
}

func TestFormat_NoNewlineAtEOF(t *testing.T) {
	a := []string{"no newline"}
	b := []string{"has newline\n"}
	ops := ComputeOps(a, b)
	u := ToUnified("a", "b", a, b, ops)
	out := FormatString(u)

	if !strings.Contains(out, "\\ No newline at end of file") {
		t.Fatalf("expected 'No newline' marker, got:\n%s", out)
	}
}

func TestRoundTrip_LargeChange(t *testing.T) {
	var a, b []string
	for i := 0; i < 50; i++ {
		a = append(a, "old line\n")
	}
	for i := 0; i < 50; i++ {
		if i == 25 {
			b = append(b, "new line\n")
		} else {
			b = append(b, "old line\n")
		}
	}

	ops := ComputeOps(a, b)
	u := ToUnified("old", "new", a, b, ops)
	out := FormatString(u)

	if !strings.Contains(out, "-old line\n") {
		t.Fatalf("expected -old line in output, got:\n%s", out)
	}
	if !strings.Contains(out, "+new line\n") {
		t.Fatalf("expected +new line in output, got:\n%s", out)
	}
}

func TestMergeOps_Adjacent(t *testing.T) {
	ops := []Op{
		{Kind: Delete, I1: 0, I2: 1, J1: 0, J2: 0},
		{Kind: Delete, I1: 1, I2: 2, J1: 0, J2: 0},
		{Kind: Insert, I1: 2, I2: 2, J1: 0, J2: 1},
		{Kind: Insert, I1: 2, I2: 2, J1: 1, J2: 2},
	}
	merged := mergeOps(ops)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged ops, got %d: %+v", len(merged), merged)
	}
	if merged[0].I1 != 0 || merged[0].I2 != 2 {
		t.Fatalf("expected delete [0:2], got [%d:%d]", merged[0].I1, merged[0].I2)
	}
	if merged[1].J1 != 0 || merged[1].J2 != 2 {
		t.Fatalf("expected insert [0:2], got [%d:%d]", merged[1].J1, merged[1].J2)
	}
}

func TestGroupPositions(t *testing.T) {
	tests := []struct {
		name      string
		positions []int
		maxGap    int
		expected  [][]int
	}{
		{"single group", []int{1, 2, 3}, 5, [][]int{{1, 2, 3}}},
		{"two groups", []int{1, 2, 10, 11}, 5, [][]int{{1, 2}, {10, 11}}},
		{"exactly at gap", []int{1, 6}, 5, [][]int{{1, 6}}},
		{"just over gap", []int{1, 7}, 5, [][]int{{1}, {7}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := groupPositions(tt.positions, tt.maxGap)
			if diff := cmp.Diff(tt.expected, result); diff != "" {
				t.Fatalf("groupPositions mismatch (-expected +got):\n%s", diff)
			}
		})
	}
}
