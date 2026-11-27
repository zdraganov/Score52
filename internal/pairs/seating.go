// Package pairs handles what lies between sessions of a pairs event: who sits where in the next
// session (so that the fields - the "streams" - mix and opponents do not repeat), and the
// qualification into finals with carry-over.
package pairs

import (
	"math/rand/v2"
	"sort"

	"github.com/zdraganov/Score52/internal/movement"
)

// History is what earlier sessions did: how often two pairs met, and how often they were in the same
// field (scored against each other: same line of a two-winner Mitchell, or the same Howell).
type History struct {
	Met       map[[2]int]int
	SameField map[[2]int]int
}

func NewHistory() History { return History{Met: map[[2]int]int{}, SameField: map[[2]int]int{}} }

func key(a, b int) [2]int {
	if a > b {
		a, b = b, a
	}
	return [2]int{a, b}
}

// Add records a played session: m's pair numbers are seat[i] = the event's pair at movement number i+1.
func (h History) Add(m movement.Movement, seat []int) {
	for _, s := range m.Seats {
		if s.NS == 0 || s.EW == 0 || s.NS == m.Phantom || s.EW == m.Phantom {
			continue
		}
		h.Met[key(seat[s.NS-1], seat[s.EW-1])]++
	}
	for _, f := range fields(m) {
		for i := range f {
			for j := i + 1; j < len(f); j++ {
				h.SameField[key(seat[f[i]-1], seat[f[j]-1])]++
			}
		}
	}
}

// fields are the movement pair numbers ranked together.
func fields(m movement.Movement) [][]int {
	all := func(from, to int) []int {
		var f []int
		for p := from; p <= to; p++ {
			if p != m.Phantom {
				f = append(f, p)
			}
		}
		return f
	}
	if ns, ew := m.Lines(); m.TwoFields {
		return [][]int{ns, ew}
	}
	return [][]int{all(1, m.Pairs)}
}

// Weights of the seating search: a repeated meeting costs Meet, two pairs again in the same field
// cost Field. For a final where meeting again is fine, set Meet to 0.
type Weights struct{ Meet, Field float64 }

var DefaultWeights = Weights{Meet: 100, Field: 1}

// Seat places the event's pairs onto movement m for the next session so that as few as possible meet
// again and the fields mix. `order` is the pairs in the order to start from (by standings, say);
// the result is seat[i] = the pair at movement number i+1 (0 for the phantom's place).
// The search is deterministic for a given seed.
func Seat(m movement.Movement, order []int, h History, w Weights, seed uint64) []int {
	n := m.Pairs
	seat := make([]int, n)
	// start: the order snaked over the fields, so strong pairs are spread
	fs := fields(m)
	var slots []int
	for i := 0; ; i++ {
		added := false
		for k := range fs {
			f := fs[k]
			if i%2 == 1 { // snake
				f = fs[len(fs)-1-k]
			}
			if i < len(f) {
				slots = append(slots, f[i])
				added = true
			}
		}
		if !added {
			break
		}
	}
	for i, p := range order {
		if i < len(slots) {
			seat[slots[i]-1] = p
		}
	}

	type edge struct{ a, b int }
	var meets []edge
	for _, s := range m.Seats {
		if s.NS != 0 && s.EW != 0 && s.NS != m.Phantom && s.EW != m.Phantom {
			meets = append(meets, edge{s.NS - 1, s.EW - 1})
		}
	}
	fieldOf := make([]int, n)
	for k, f := range fs {
		for _, p := range f {
			fieldOf[p-1] = k
		}
	}
	cost := func() float64 {
		var c float64
		for _, e := range meets {
			c += w.Meet * float64(h.Met[key(seat[e.a], seat[e.b])])
		}
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if fieldOf[i] == fieldOf[j] && seat[i] != 0 && seat[j] != 0 {
					c += w.Field * float64(h.SameField[key(seat[i], seat[j])])
				}
			}
		}
		return c
	}

	movable := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if i+1 != m.Phantom {
			movable = append(movable, i)
		}
	}
	rng := rand.New(rand.NewPCG(seed, 0x5eed))
	best := cost()
	for it := 0; it < 4000*n && best > 0; it++ {
		a, b := movable[rng.IntN(len(movable))], movable[rng.IntN(len(movable))]
		if a == b {
			continue
		}
		seat[a], seat[b] = seat[b], seat[a]
		if c := cost(); c <= best {
			best = c
		} else {
			seat[a], seat[b] = seat[b], seat[a]
		}
	}
	return seat
}

// Repeats counts the meetings of a planned seating that already happened.
func Repeats(m movement.Movement, seat []int, h History) int {
	n := 0
	for _, s := range m.Seats {
		if s.NS != 0 && s.EW != 0 && s.NS != m.Phantom && s.EW != m.Phantom && h.Met[key(seat[s.NS-1], seat[s.EW-1])] > 0 {
			n++
		}
	}
	return n
}

// sortedCopy is a helper for tests and callers that want a stable order.
func sortedCopy(v []int) []int {
	out := append([]int(nil), v...)
	sort.Ints(out)
	return out
}
