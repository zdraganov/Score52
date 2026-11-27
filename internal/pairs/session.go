package pairs

import (
	"fmt"
	"sort"

	"github.com/zdraganov/Score52/internal/movement"
	"github.com/zdraganov/Score52/internal/score"
)

// Method is how a pairs session scores its boards.
type Method string

const (
	MethodMP       Method = "mp"
	MethodCrossIMP Method = "cross-imp"
	MethodButler   Method = "butler"
)

// Line is a traveller line by movement pair numbers. Played false is an artificial adjusted score:
// AdjNS / AdjEW are percentages for MP and IMPs otherwise.
type Line struct {
	Board, NS, EW int
	Played        bool
	Score         int // NS
	AdjNS, AdjEW  float64
}

// LineScore is a line's result for each side: a percentage (MP) or IMPs.
type LineScore struct {
	Board, NS, EW      int
	NSPoints, EWPoints float64
	MPNS, MPEW         float64 // MP only, on the 2-per-beat scale
	Top                float64 // MP only
}

// SessionResult is a scored session: every line, and per movement pair number its total.
type SessionResult struct {
	Lines  []LineScore
	Totals map[int]SessionScore
}

// ScoreSession scores a session's lines on movement m (boards per round bpr, numbered from first).
// A board's comparisons are the times the movement plays it, so a missing result is factored up
// (Neuberg) instead of counting as an average. butlerTrim is the Butler datum's trim (0.1 is usual).
func ScoreSession(m movement.Movement, bpr, first int, method Method, butlerTrim float64, lines []Line) (SessionResult, error) {
	expected := map[int]int{}
	for _, s := range m.Seats {
		if s.NS == 0 || s.EW == 0 || s.NS == m.Phantom || s.EW == m.Phantom {
			continue
		}
		for i := 0; i < bpr; i++ {
			expected[first+(s.Set-1)*bpr+i]++
		}
	}
	byBoard := map[int][]Line{}
	for _, l := range lines {
		if expected[l.Board] == 0 {
			return SessionResult{}, fmt.Errorf("board %d is not played in this movement", l.Board)
		}
		byBoard[l.Board] = append(byBoard[l.Board], l)
	}
	var boards []int
	for b := range byBoard {
		boards = append(boards, b)
	}
	sort.Ints(boards)

	out := SessionResult{Totals: map[int]SessionScore{}}
	add := func(pair int, pts, top float64) {
		t := out.Totals[pair]
		t.Pair = pair
		t.Score += pts
		t.Top += top
		t.Boards++
		out.Totals[pair] = t
	}
	for _, b := range boards {
		ls := byBoard[b]
		rs := make([]score.Result, len(ls))
		for i, l := range ls {
			rs[i] = score.Result{NS: l.NS, EW: l.EW, Played: l.Played, Score: l.Score, AdjNS: l.AdjNS, AdjEW: l.AdjEW}
		}
		switch method {
		case MethodMP:
			top := 2 * float64(expected[b]-1)
			for _, r := range score.Matchpoints(rs, expected[b]) {
				out.Lines = append(out.Lines, LineScore{Board: b, NS: r.NS, EW: r.EW, NSPoints: r.PctNS, EWPoints: r.PctEW, MPNS: r.MPNS, MPEW: r.MPEW, Top: top})
				add(r.NS, r.MPNS, top)
				add(r.EW, r.MPEW, top)
			}
		case MethodCrossIMP, MethodButler:
			var ir []score.IMPResult
			if method == MethodButler {
				ir, _ = score.Butler(rs, butlerTrim)
			} else {
				ir = score.CrossIMP(rs)
			}
			for _, r := range ir {
				out.Lines = append(out.Lines, LineScore{Board: b, NS: r.NS, EW: r.EW, NSPoints: r.IMPNS, EWPoints: r.IMPEW})
				add(r.NS, r.IMPNS, 0)
				add(r.EW, r.IMPEW, 0)
			}
		default:
			return SessionResult{}, fmt.Errorf("unknown scoring %q", method)
		}
	}
	return out, nil
}
