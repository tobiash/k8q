package diff

import (
	"fmt"
	"io"
	"strings"
)

type OpKind int

const (
	Delete OpKind = iota
	Insert
	Equal
)

type Op struct {
	Kind           OpKind
	I1, I2, J1, J2 int
}

type Line struct {
	Kind    OpKind
	Content string
}

type Hunk struct {
	FromLine int
	ToLine   int
	Lines    []Line
}

type Unified struct {
	From  string
	To    string
	Hunks []*Hunk
}

const contextLines = 3

func ComputeOps(a, b []string) []Op {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}

	M, N := len(a), len(b)
	V := make([]int, 2*(N+M)+1)
	offset := N + M

	var trace [][]int

	for d := 0; d <= N+M; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && V[k-1+offset] < V[k+1+offset]) {
				x = V[k+1+offset]
			} else {
				x = V[k-1+offset] + 1
			}
			y := x - k
			for x < M && y < N && a[x] == b[y] {
				x++
				y++
			}
			V[k+offset] = x
			if x == M && y == N {
				copyV := make([]int, len(V))
				copy(copyV, V)
				trace = append(trace, copyV)
				return opsFromTrace(a, b, trace, offset)
			}
		}
		copyV := make([]int, len(V))
		copy(copyV, V)
		trace = append(trace, copyV)
	}

	return opsFromTrace(a, b, trace, offset)
}

type snake struct{ x, y int }

func opsFromTrace(a, b []string, trace [][]int, offset int) []Op {
	M, N := len(a), len(b)

	snakes := make([]snake, len(trace)+1)

	x, y := M, N
	for d := len(trace) - 1; d > 0; d-- {
		V := trace[d]
		snakes[d] = snake{x, y}

		k := x - y
		var kPrev int
		if k == -d || (k != d && V[k-1+offset] < V[k+1+offset]) {
			kPrev = k + 1
		} else {
			kPrev = k - 1
		}

		x = V[kPrev+offset]
		y = x - kPrev
	}
	snakes[0] = snake{x, y}

	return snakesToOps(a, b, snakes)
}

func snakesToOps(a, b []string, snakes []snake) []Op {
	M, N := len(a), len(b)
	var ops []Op
	x, y := 0, 0

	for d := 0; d < len(snakes); d++ {
		sx, sy := snakes[d].x, snakes[d].y

		for sx-sy > x-y && x < M {
			ops = append(ops, Op{Kind: Delete, I1: x, I2: x + 1, J1: y, J2: y})
			x++
		}

		for sx-sy < x-y && y < N {
			ops = append(ops, Op{Kind: Insert, I1: x, I2: x, J1: y, J2: y + 1})
			y++
		}

		for x < sx {
			x++
			y++
		}

		if x >= M && y >= N {
			break
		}
	}

	return mergeOps(ops)
}

type lineItem struct {
	kind    OpKind
	content string
	aLine   int
	bLine   int
}

func ToUnified(from, to string, a, b []string, ops []Op) Unified {
	u := Unified{From: from, To: to}
	if len(ops) == 0 {
		return u
	}

	items := buildItems(a, b, ops)

	changePositions := make([]int, 0)
	for i, item := range items {
		if item.kind != Equal {
			changePositions = append(changePositions, i)
		}
	}
	if len(changePositions) == 0 {
		return u
	}

	groups := groupPositions(changePositions, contextLines*2+1)

	for _, group := range groups {
		start := group[0] - contextLines
		if start < 0 {
			start = 0
		}
		end := group[len(group)-1] + contextLines + 1
		if end > len(items) {
			end = len(items)
		}

		h := &Hunk{
			FromLine: items[start].aLine,
			ToLine:   items[start].bLine,
		}

		for i := start; i < end; i++ {
			h.Lines = append(h.Lines, Line{
				Kind:    items[i].kind,
				Content: items[i].content,
			})
		}

		u.Hunks = append(u.Hunks, h)
	}

	return u
}

func buildItems(a, b []string, ops []Op) []lineItem {
	var items []lineItem
	ai, bi := 0, 0

	for _, op := range ops {
		for ai < op.I1 && bi < op.J1 {
			items = append(items, lineItem{Equal, a[ai], ai + 1, bi + 1})
			ai++
			bi++
		}
		switch op.Kind {
		case Delete:
			for i := op.I1; i < op.I2; i++ {
				items = append(items, lineItem{Delete, a[i], ai + 1, 0})
				ai++
			}
		case Insert:
			for j := op.J1; j < op.J2; j++ {
				items = append(items, lineItem{Insert, b[j], 0, bi + 1})
				bi++
			}
		}
	}

	for ai < len(a) && bi < len(b) {
		items = append(items, lineItem{Equal, a[ai], ai + 1, bi + 1})
		ai++
		bi++
	}

	return items
}

func groupPositions(positions []int, maxGap int) [][]int {
	var groups [][]int
	current := []int{positions[0]}
	for i := 1; i < len(positions); i++ {
		if positions[i]-current[len(current)-1] <= maxGap {
			current = append(current, positions[i])
		} else {
			groups = append(groups, current)
			current = []int{positions[i]}
		}
	}
	groups = append(groups, current)
	return groups
}

func Format(w io.Writer, u Unified) {
	if len(u.Hunks) == 0 {
		return
	}
	fmt.Fprintf(w, "--- %s\n", u.From)
	fmt.Fprintf(w, "+++ %s\n", u.To)
	for _, hunk := range u.Hunks {
		fromCount, toCount := 0, 0
		for _, l := range hunk.Lines {
			switch l.Kind {
			case Delete:
				fromCount++
			case Insert:
				toCount++
			default:
				fromCount++
				toCount++
			}
		}
		fmt.Fprint(w, "@@")
		if fromCount > 1 {
			fmt.Fprintf(w, " -%d,%d", hunk.FromLine, fromCount)
		} else {
			fmt.Fprintf(w, " -%d", hunk.FromLine)
		}
		if toCount > 1 {
			fmt.Fprintf(w, " +%d,%d", hunk.ToLine, toCount)
		} else {
			fmt.Fprintf(w, " +%d", hunk.ToLine)
		}
		fmt.Fprint(w, " @@\n")
		for _, l := range hunk.Lines {
			prefix := ' '
			switch l.Kind {
			case Delete:
				prefix = '-'
			case Insert:
				prefix = '+'
			}
			fmt.Fprintf(w, "%c%s", prefix, l.Content)
			if !strings.HasSuffix(l.Content, "\n") {
				fmt.Fprint(w, "\n\\ No newline at end of file\n")
			}
		}
	}
}

func FormatString(u Unified) string {
	var sb strings.Builder
	Format(&sb, u)
	return sb.String()
}

func mergeOps(ops []Op) []Op {
	if len(ops) == 0 {
		return ops
	}
	merged := []Op{ops[0]}
	for i := 1; i < len(ops); i++ {
		last := &merged[len(merged)-1]
		cur := ops[i]
		if cur.Kind == last.Kind && cur.I1 == last.I2 && cur.J1 == last.J2 {
			last.I2 = cur.I2
			last.J2 = cur.J2
			continue
		}
		merged = append(merged, cur)
	}
	return merged
}
