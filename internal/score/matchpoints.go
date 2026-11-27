package score

import (
	"math"
	"sort"
)

// Result is one table's result on a board, from NS's point of view.
//
// A played board has Played set and its NS score. A board that could not be played gets an
// artificial adjusted score instead: AdjNS and AdjEW are percentages (60/40, 50/50, 40/40...),
// which the two pairs receive whatever the other tables did, and which do not count as a
// comparison for the others (they are factored up with Neuberg instead).
type Result struct {
	NS, EW int // pair ids
	Played bool
	Score  int // NS score, when Played
	AdjNS  float64
	AdjEW  float64
}

// MP is a pair's matchpoint result on a board.
type MP struct {
	NS, EW     int
	MPNS, MPEW float64 // on the 2-per-beat scale: top is 2*(expected-1)
	PctNS      float64 // 0..100
	PctEW      float64
}

// Matchpoints scores one board's results. expected is how many times the board should have been
// played (the number of tables in its section); when fewer real scores exist, each is factored up
// with the Neuberg formula so that a board played 6 times weighs as much as one played 8 times.
func Matchpoints(results []Result, expected int) []MP {
	var real []int // indices of played results
	for i, r := range results {
		if r.Played {
			real = append(real, i)
		}
	}
	if expected < len(real) {
		expected = len(real)
	}
	top := 2 * float64(expected-1)
	out := make([]MP, len(results))
	for i, r := range results {
		out[i].NS, out[i].EW = r.NS, r.EW
		if !r.Played {
			out[i].PctNS, out[i].PctEW = r.AdjNS, r.AdjEW
			out[i].MPNS, out[i].MPEW = r.AdjNS/100*top, r.AdjEW/100*top
			continue
		}
		var mp float64 // 2 per score beaten, 1 per tie
		for _, j := range real {
			if j == i {
				continue
			}
			switch o := results[j].Score; {
			case r.Score > o:
				mp += 2
			case r.Score == o:
				mp++
			}
		}
		n := float64(len(real))
		if n < float64(expected) && n > 0 {
			// Neuberg: (MP + 1) * E/n - 1 on the 2-point scale
			mp = (mp+1)*float64(expected)/n - 1
		}
		out[i].MPNS = mp
		out[i].MPEW = top - mp
		if top > 0 {
			out[i].PctNS = 100 * mp / top
		} else {
			out[i].PctNS = 50
		}
		out[i].PctEW = 100 - out[i].PctNS
	}
	return out
}

// IMPResult is a pair's IMP result on a board (cross-IMP or Butler).
type IMPResult struct {
	NS, EW int
	IMPNS  float64
	IMPEW  float64
}

// CrossIMP scores each played result against every other one and averages: the sum of IMPs over
// the comparisons divided by their number. Adjusted results get AdjNS/AdjEW read as IMPs (e.g. +3/-3).
func CrossIMP(results []Result) []IMPResult {
	out := make([]IMPResult, len(results))
	for i, r := range results {
		out[i].NS, out[i].EW = r.NS, r.EW
		if !r.Played {
			out[i].IMPNS, out[i].IMPEW = r.AdjNS, r.AdjEW
			continue
		}
		var sum, n float64
		for j, o := range results {
			if j == i || !o.Played {
				continue
			}
			sum += float64(IMP(r.Score - o.Score))
			n++
		}
		if n > 0 {
			sum /= n
		}
		out[i].IMPNS, out[i].IMPEW = sum, -sum
	}
	return out
}

// Butler scores each played result in IMPs against a datum: the mean NS score after dropping
// the trim fraction of highest and lowest scores (e.g. 0.1 drops the top and bottom 10%, at
// least one each when there are 5+ results), rounded to the nearest 10.
func Butler(results []Result, trim float64) ([]IMPResult, int) {
	var scores []int
	for _, r := range results {
		if r.Played {
			scores = append(scores, r.Score)
		}
	}
	datum := Datum(scores, trim)
	out := make([]IMPResult, len(results))
	for i, r := range results {
		out[i].NS, out[i].EW = r.NS, r.EW
		if !r.Played {
			out[i].IMPNS, out[i].IMPEW = r.AdjNS, r.AdjEW
			continue
		}
		v := float64(IMP(r.Score - datum))
		out[i].IMPNS, out[i].IMPEW = v, -v
	}
	return out, datum
}

// Datum is the Butler datum of a set of NS scores.
func Datum(scores []int, trim float64) int {
	if len(scores) == 0 {
		return 0
	}
	s := append([]int(nil), scores...)
	sort.Ints(s)
	k := int(math.Floor(float64(len(s)) * trim))
	if k == 0 && trim > 0 && len(s) >= 5 {
		k = 1
	}
	s = s[k : len(s)-k]
	var sum float64
	for _, v := range s {
		sum += float64(v)
	}
	return int(math.Round(sum/float64(len(s))/10) * 10)
}
