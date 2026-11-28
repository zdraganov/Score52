package teams

import (
	"errors"
	"fmt"
)

// errFinished: the group has nothing left to play (its knockout has a winner, its round robin is
// complete); it sits the round out while the others play.
var errFinished = errors.New("finished")

// PairRound makes the next round's matches in every group and adds them to the event.
// Every group plays the same number of boards.
func (e *Event) PairRound(boards int) ([]Match, error) {
	return e.PairRoundBoards(boards, nil)
}

// PairRoundBoards is PairRound with a number of boards per group where it differs from `boards`
// (a knockout's 16 while the Swiss below plays 8).
func (e *Event) PairRoundBoards(boards int, per map[string]int) ([]Match, error) {
	if err := e.allPlayed(); err != nil {
		return nil, err
	}
	op := Op{Kind: OpPair}
	round := e.Round + 1
	for _, g := range e.Groups {
		pairs, order, err := e.pairGroup(g)
		if errors.Is(err, errFinished) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("group %s: %w", g.Name, err)
		}
		n := boards
		if v, ok := per[g.Name]; ok && v > 0 {
			n = v
		}
		for _, p := range pairs {
			op.Matches = append(op.Matches, Match{Round: round, Group: g.Name, Home: p[0], Away: p[1], Boards: n})
		}
		if order != nil {
			if op.RROrder == nil {
				op.RROrder = map[string][]TeamID{}
			}
			op.RROrder[g.Name] = order
		}
	}
	if len(op.Matches) == 0 {
		return nil, errors.New("every group has finished: nothing left to pair")
	}
	if err := e.apply(op); err != nil {
		return nil, err
	}
	return e.Matches[len(e.Matches)-len(op.Matches):], nil
}

// pairGroup decides a group's next round; a round robin starting its schedule also gives its order.
func (e *Event) pairGroup(g *Group) ([][2]TeamID, []TeamID, error) {
	var ranked []TeamID
	for _, s := range e.Standings(g.Name) {
		if !e.eliminated(g, s.Team) {
			ranked = append(ranked, s.Team)
		}
	}
	switch g.Format {
	case Swiss:
		if e.Round == 0 {
			return e.foldBySeed(ranked), nil, nil
		}
		p, err := e.swiss(ranked)
		return p, nil, err
	case Danish:
		return e.danish(ranked), nil, nil
	case RoundRobin:
		return e.roundRobin(g, ranked)
	case Knockout:
		p, err := e.knockout(g, ranked)
		return p, nil, err
	}
	return nil, nil, fmt.Errorf("unknown format %q", g.Format)
}

// met counts how many times two teams have played each other in the event.
func (e *Event) met() map[[2]TeamID]int {
	out := map[[2]TeamID]int{}
	for _, m := range e.Matches {
		if !m.Bye() {
			out[tkey(m.Home, m.Away)]++
		}
	}
	return out
}

func tkey(a, b TeamID) [2]TeamID {
	if a > b {
		a, b = b, a
	}
	return [2]TeamID{a, b}
}

// takeBye removes the team that sits out an odd round: the lowest ranked one without a bye yet.
func (e *Event) takeBye(ranked []TeamID) ([]TeamID, *[2]TeamID) {
	if len(ranked)%2 == 0 {
		return ranked, nil
	}
	byes := map[TeamID]int{}
	for _, m := range e.Matches {
		if m.Bye() {
			byes[m.Home]++
		}
	}
	pick := len(ranked) - 1
	for i := len(ranked) - 2; i >= 0; i-- {
		if byes[ranked[i]] < byes[ranked[pick]] {
			pick = i
		}
	}
	bye := [2]TeamID{ranked[pick], 0}
	rest := append(append([]TeamID(nil), ranked[:pick]...), ranked[pick+1:]...)
	return rest, &bye
}

// foldBySeed is a first Swiss round: by seed, the top half against the bottom half (1-N/2+1, 2-N/2+2...).
func (e *Event) foldBySeed(ids []TeamID) [][2]TeamID {
	bySeed := append([]TeamID(nil), ids...)
	sortBy(bySeed, func(a, b TeamID) bool { return e.Teams[a].Seed < e.Teams[b].Seed })
	rest, bye := e.takeBye(bySeed)
	h := len(rest) / 2
	var out [][2]TeamID
	for i := 0; i < h; i++ {
		out = append(out, [2]TeamID{rest[i], rest[i+h]})
	}
	if bye != nil {
		out = append(out, *bye)
	}
	return out
}

// swiss pairs from the top: each team against the highest ranked team it has not met. When that
// paints the bottom into a corner, it backtracks; when no pairing without repeats exists (a small
// closed group late in the event), it allows teams that have met once to meet again, then twice...,
// and among those picks the one closest to the plain top-down order.
func (e *Event) swiss(ranked []TeamID) ([][2]TeamID, error) {
	rest, bye := e.takeBye(ranked)
	met := e.met()
	for allow := 0; allow <= len(rest); allow++ {
		if p := swissSearch(rest, met, allow); p != nil {
			if bye != nil {
				p = append(p, *bye)
			}
			return p, nil
		}
	}
	return nil, fmt.Errorf("no Swiss pairing")
}

func swissSearch(ranked []TeamID, met map[[2]TeamID]int, allow int) [][2]TeamID {
	used := make([]bool, len(ranked))
	var out [][2]TeamID
	var rec func() bool
	rec = func() bool {
		i := 0
		for i < len(ranked) && used[i] {
			i++
		}
		if i == len(ranked) {
			return true
		}
		used[i] = true
		for j := i + 1; j < len(ranked); j++ {
			if used[j] || met[tkey(ranked[i], ranked[j])] > allow {
				continue
			}
			used[j] = true
			out = append(out, [2]TeamID{ranked[i], ranked[j]})
			if rec() {
				return true
			}
			out = out[:len(out)-1]
			used[j] = false
		}
		used[i] = false
		return false
	}
	if rec() {
		return out
	}
	return nil
}

// danish pairs 1-2, 3-4... whether or not they have met.
func (e *Event) danish(ranked []TeamID) [][2]TeamID {
	rest, bye := e.takeBye(ranked)
	var out [][2]TeamID
	for i := 0; i+1 < len(rest); i += 2 {
		out = append(out, [2]TeamID{rest[i], rest[i+1]})
	}
	if bye != nil {
		out = append(out, *bye)
	}
	return out
}

// roundRobin plays the group's Berger schedule (circle method), fixed in rank order when the group
// starts; an odd group has a bye each round. It returns the order when it fixes it.
func (e *Event) roundRobin(g *Group, ranked []TeamID) ([][2]TeamID, []TeamID, error) {
	order, fresh := g.order, false
	if order == nil {
		order, fresh = append([]TeamID(nil), ranked...), true
		if len(order)%2 == 1 {
			order = append(order, 0)
		}
	}
	n := len(order)
	r := g.rrRound
	if r >= n-1 {
		return nil, nil, fmt.Errorf("the round robin is complete (%d rounds): %w", n-1, errFinished)
	}
	var out [][2]TeamID
	// position 0 fixed, the others rotate
	pos := func(i int) TeamID {
		if i == 0 {
			return order[0]
		}
		return order[1+mod(i-1+r, n-1)]
	}
	for i := 0; i < n/2; i++ {
		a, b := pos(i), pos(n-1-i)
		if r%2 == 1 && i == 0 {
			a, b = b, a
		}
		if a == 0 {
			a, b = b, a
		}
		out = append(out, [2]TeamID{a, b})
	}
	if !fresh {
		order = nil
	}
	return out, order, nil
}

// knockout seeds the group's first round by rank in bracket order (1-8, 4-5, 2-7, 3-6 for eight), then
// pairs the winners match by match; the losers drop out of the group.
func (e *Event) knockout(g *Group, ranked []TeamID) ([][2]TeamID, error) {
	var prev []Match
	last := 0
	for _, m := range e.Matches {
		if m.Group == g.Name && m.Knockout && m.Round > g.since && m.Round > last {
			last = m.Round
		}
	}
	for _, m := range e.Matches {
		if m.Group == g.Name && m.Knockout && m.Round == last {
			prev = append(prev, m)
		}
	}
	if len(prev) == 0 {
		n := len(ranked)
		size := 1
		for size < n {
			size *= 2
		}
		order := bracket(size) // seeds 1..size; seeds above n are byes
		var out [][2]TeamID
		for i := 0; i < len(order); i += 2 {
			a, b := order[i], order[i+1]
			switch {
			case a <= n && b <= n:
				out = append(out, [2]TeamID{ranked[a-1], ranked[b-1]})
			case a <= n:
				out = append(out, [2]TeamID{ranked[a-1], 0})
			case b <= n:
				out = append(out, [2]TeamID{ranked[b-1], 0})
			}
		}
		return out, nil
	}
	var winners []TeamID
	for _, m := range prev {
		w := m.Home
		if h, a := m.KOTotal(); !m.Bye() && a > h {
			w = m.Away
		}
		winners = append(winners, w)
	}
	if len(winners) < 2 {
		return nil, fmt.Errorf("the knockout is over: %w", errFinished)
	}
	var out [][2]TeamID
	for i := 0; i+1 < len(winners); i += 2 {
		out = append(out, [2]TeamID{winners[i], winners[i+1]})
	}
	return out, nil
}

// bracket is the standard seeding order for a knockout of size 2^k: 1,8,4,5,2,7,3,6 for eight.
func bracket(size int) []int {
	order := []int{1, 2}
	for len(order) < size {
		n := len(order) * 2
		var next []int
		for _, s := range order {
			next = append(next, s, n+1-s)
		}
		order = next
	}
	return order
}

func mod(a, n int) int { return ((a % n) + n) % n }

func sortBy(ids []TeamID, less func(a, b TeamID) bool) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && less(ids[j], ids[j-1]); j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}
