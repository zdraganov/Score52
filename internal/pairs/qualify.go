package pairs

import (
	"fmt"
	"sort"
)

// Scoring of a pairs event.
type Scoring string

const (
	MP  Scoring = "mp"  // matchpoints: results are percentages
	IMP Scoring = "imp" // cross-IMP or Butler: results are IMPs
)

// SessionScore is a pair's result in one session. For MP, Score is the matchpoints and Top their
// maximum (so sessions of different length weigh by boards); for IMP, Score is IMPs and Top unused.
type SessionScore struct {
	Pair   int
	Score  float64
	Top    float64
	Boards int
}

// Total is a pair's result over a stage: a percentage for MP, IMPs for IMP, plus its carry-over.
type Total struct {
	Pair  int
	Carry float64
	Score float64 // over the stage's sessions
	Total float64 // Carry + Score (MP: weighted, see Carry)
	Rank  int
}

// Carry turns a qualifying result into a final's starting score.
//
// For IMPs the final starts from Fraction * qualifying IMPs. For MP the final's total is the
// weighted percentage Fraction*qualifying% + (1-Fraction)*final% - a 25% carry-over means the
// qualifying counts a quarter. Fraction 0 is no carry-over.
type Carry struct{ Fraction float64 }

// Totals adds up sessions for a scoring, applying the carry of each pair (pair -> carried score).
func Totals(scoring Scoring, sessions [][]SessionScore, carry map[int]float64, c Carry) []Total {
	type acc struct{ s, top float64 }
	by := map[int]*acc{}
	for _, ss := range sessions {
		for _, s := range ss {
			a := by[s.Pair]
			if a == nil {
				a = &acc{}
				by[s.Pair] = a
			}
			a.s += s.Score
			a.top += s.Top
		}
	}
	var out []Total
	for p, a := range by {
		t := Total{Pair: p, Carry: carry[p]}
		switch scoring {
		case MP:
			if a.top > 0 {
				t.Score = 100 * a.s / a.top
			}
			if c.Fraction > 0 {
				t.Total = c.Fraction*t.Carry + (1-c.Fraction)*t.Score
			} else {
				t.Total = t.Score
			}
		default:
			t.Score = a.s
			t.Total = c.Fraction*t.Carry + t.Score
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].Pair < out[j].Pair
	})
	for i := range out {
		out[i].Rank = i + 1
		if i > 0 && out[i].Total == out[i-1].Total {
			out[i].Rank = out[i-1].Rank
		}
	}
	return out
}

// Final is one final a qualification fills: the next Count pairs of the ranking (0 = the rest).
type Final struct {
	Name  string // "A", "B"...
	Count int
}

// Qualify splits a stage's ranking into finals. Each final's carry map holds the qualifying score
// (Total) for the next stage's Totals. A tie across a cut is an error for the director to settle.
func Qualify(ranking []Total, finals []Final) (map[string][]int, map[int]float64, error) {
	out := map[string][]int{}
	carry := map[int]float64{}
	at := 0
	for i, f := range finals {
		n := f.Count
		if n == 0 || (i == len(finals)-1 && at+n > len(ranking)) {
			n = len(ranking) - at
		}
		if at+n > len(ranking) {
			return nil, nil, fmt.Errorf("final %s: only %d pairs left", f.Name, len(ranking)-at)
		}
		if end := at + n; end < len(ranking) && end > 0 && ranking[end].Total == ranking[end-1].Total {
			return nil, nil, fmt.Errorf("final %s: pairs %d and %d tie on the cut", f.Name, ranking[end-1].Pair, ranking[end].Pair)
		}
		for _, t := range ranking[at : at+n] {
			out[f.Name] = append(out[f.Name], t.Pair)
			carry[t.Pair] = t.Total
		}
		at += n
	}
	return out, carry, nil
}
