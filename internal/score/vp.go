package score

import "math"

// VPScale converts a team match's IMP margin into victory points for both teams.
type VPScale interface {
	VP(imps float64, boards int) (winner, loser float64)
}

// WBF20 is the WBF continuous 20-point scale (2013): for a margin M over N boards,
// with B = 15*sqrt(N) and tau = (sqrt5-1)/2, the winner gets 10 + 10*(1-tau^(3M/B))/(1-tau^3),
// at most 20, rounded to 2 decimals; the loser the rest of 20.
type WBF20 struct{}

var tau = (math.Sqrt(5) - 1) / 2

func (WBF20) VP(imps float64, boards int) (float64, float64) {
	m := math.Abs(imps)
	b := 15 * math.Sqrt(float64(boards))
	v := 10 + 10*(1-math.Pow(tau, 3*m/b))/(1-math.Pow(tau, 3))
	if v > 20 {
		v = 20
	}
	v = math.Round(v*100) / 100
	return v, math.Round((20-v)*100) / 100
}

// MatchVP gives each team its VPs for a margin from team A's point of view.
func MatchVP(s VPScale, impsA float64, boards int) (vpA, vpB float64) {
	w, l := s.VP(impsA, boards)
	if impsA < 0 {
		return l, w
	}
	return w, l
}

// TeamBoardIMP is team A's IMPs on a board: A sat NS at the open table and EW at the closed one,
// so the difference of the two NS scores is A's gain.
func TeamBoardIMP(openNS, closedNS int) int { return IMP(openNS - closedNS) }

// BAM is team A's board-a-match points on a board: 1 for a better net score, 1/2 for the same, 0 worse.
func BAM(openNS, closedNS int) float64 {
	switch d := openNS - closedNS; {
	case d > 0:
		return 1
	case d == 0:
		return 0.5
	default:
		return 0
	}
}
