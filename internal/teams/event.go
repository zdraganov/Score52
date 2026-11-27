// Package teams runs a multi-stage teams event: rounds of Swiss, Danish, round robin or knockout,
// in groups (A, B, C...) whose membership can change between rounds - a field split into groups,
// a group closed with its top N, teams exchanged between groups - with a carry-over rule per group.
//
// Every change is an Op, applied by one function and kept in Event.Log, so an event is saved as its
// log and rebuilt by Replay. Ops hold decisions as made (who was paired, who went to which group), not
// the inputs to them: a corrected score never re-decides a pairing or a group. Standings and
// carry-overs are worked out from the matches every time, so a correction reaches everything after it.
package teams

import (
	"fmt"
	"math"
	"sort"

	"bridge.bg/scorer/internal/score"
)

type TeamID int

type Team struct {
	ID   TeamID `json:"id"`
	Name string `json:"name"`
	Seed int    `json:"seed"` // 1 = strongest; breaks the last ties and seeds round 1
}

type Format string

const (
	Swiss      Format = "swiss"
	Danish     Format = "danish"
	RoundRobin Format = "round-robin"
	Knockout   Format = "knockout"
)

// CarryKind says what a team brings into a group when it joins it.
type CarryKind string

const (
	CarryAll        CarryKind = "all"          // its score so far
	CarryNone       CarryKind = "none"         // starts from 0
	CarryHeadToHead CarryKind = "head-to-head" // VPs won against the teams it joins
)

// Carry is a carry-over rule; Fraction scales it (1 when 0).
type Carry struct {
	Kind     CarryKind `json:"kind"`
	Fraction float64   `json:"fraction,omitempty"`
}

func (c Carry) factor() float64 {
	if c.Fraction == 0 {
		return 1
	}
	return c.Fraction
}

// Match is one match of a round. Away == 0 is a bye for Home.
type Match struct {
	Round  int    `json:"round"`
	Group  string `json:"group"`
	Home   TeamID `json:"home"`
	Away   TeamID `json:"away"`
	Boards int    `json:"boards"`
	// Knockout: paired in a knockout group (a winner, no VPs); kept on the match, as its group may go away
	Knockout bool `json:"knockout,omitempty"`
	// Knockout carry-over in IMPs, set by the director (SetCarry) and added to decide the winner
	CarryHome float64 `json:"carry_home,omitempty"`
	CarryAway float64 `json:"carry_away,omitempty"`
	// Knockout tie: the extra boards played to break it (AddPlayoff), added on top
	PlayoffBoards int     `json:"playoff_boards,omitempty"`
	PlayoffHome   float64 `json:"playoff_home,omitempty"`
	PlayoffAway   float64 `json:"playoff_away,omitempty"`
	// Tied: a knockout match level after its boards and carry-over, waiting for extra boards
	Tied     bool    `json:"tied,omitempty"`
	Played   bool    `json:"played,omitempty"`
	IMPsHome float64 `json:"imps_home,omitempty"`
	IMPsAway float64 `json:"imps_away,omitempty"`
	VPHome   float64 `json:"vp_home,omitempty"`
	VPAway   float64 `json:"vp_away,omitempty"`
}

// KOTotal is each side's knockout total: carry-over + IMPs + extra boards.
func (m Match) KOTotal() (home, away float64) {
	return m.CarryHome + m.IMPsHome + m.PlayoffHome, m.CarryAway + m.IMPsAway + m.PlayoffAway
}

func (m Match) Bye() bool { return m.Away == 0 }

// membership is how a team came into its group; its carry-over is worked out from it on demand.
type membership struct {
	after int      // joined after this round
	rule  Carry    // what it brought
	peers []TeamID // the group's teams as it joined (head-to-head)
	// from an exchange: the membership it left; CarryAll then means its score there, not the event's
	prev *membership
}

// Group is a set of teams playing each other in one format.
type Group struct {
	Name    string
	Format  Format
	Carry   Carry // the rule for teams put in it by a split
	since   int   // formed after this round: earlier matches under the same name are another group's
	members map[TeamID]*membership
	order   []TeamID // round robin: the schedule's order, fixed when the group is first paired
	rrRound int
}

type Event struct {
	Teams     map[TeamID]Team
	ScaleName string
	Scale     score.VPScale
	ByeVP     float64
	Matches   []Match
	Groups    []*Group // in rank order: A above B above C
	Round     int      // the last round paired
	Log       []Op
}

// Scales are the VP scales an event can name.
var Scales = map[string]score.VPScale{"wbf20": score.WBF20{}}

// New starts an event with every team in one group "" of the given format.
func New(teams []Team, format Format, scale string, byeVP float64) (*Event, error) {
	e := &Event{}
	if err := e.apply(Op{Kind: OpStart, Teams: teams, Format: format, Scale: scale, ByeVP: byeVP}); err != nil {
		return nil, err
	}
	return e, nil
}

// Replay rebuilds an event from its log.
func Replay(log []Op) (*Event, error) {
	e := &Event{}
	for i, op := range log {
		if i == 0 && op.Kind != OpStart {
			return nil, fmt.Errorf("the log does not start the event")
		}
		if err := e.apply(op); err != nil {
			return nil, fmt.Errorf("op %d (%s): %w", i+1, op.Kind, err)
		}
	}
	return e, nil
}

func (e *Event) Group(name string) *Group {
	for _, g := range e.Groups {
		if g.Name == name {
			return g
		}
	}
	return nil
}

// Members are the teams of a group still in it (a knockout's losers drop out), by id.
func (e *Event) Members(group string) []TeamID {
	g := e.Group(group)
	if g == nil {
		return nil
	}
	var out []TeamID
	for id := range g.members {
		if !e.eliminated(g, id) {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// eliminated: the team lost a knockout match of the group.
func (e *Event) eliminated(g *Group, id TeamID) bool {
	if g.Format != Knockout {
		return false
	}
	for _, m := range e.Matches {
		if m.Group != g.Name || !m.Played || m.Tied || m.Bye() || m.Round <= g.since || m.Round <= g.members[id].after {
			continue
		}
		h, a := m.KOTotal()
		if (m.Home == id && a > h) || (m.Away == id && h > a) {
			return true
		}
	}
	return false
}

// GroupSpec is one group a split makes: the next Count teams of the source's ranking (0 = all the rest).
type GroupSpec struct {
	Name   string
	Count  int
	Format Format
	Carry  Carry
}

// Split divides group `from` by its current standings into new groups, in order: "after round 8,
// group A is the top 10 and B the rest". The source group goes away; its place in the group order
// is taken by the new ones.
func (e *Event) Split(from string, specs []GroupSpec) error {
	if err := e.allPlayed(); err != nil {
		return err
	}
	if e.Group(from) == nil {
		return fmt.Errorf("no group %q", from)
	}
	ranked := e.Standings(from)
	op := Op{Kind: OpSplit, Group: from}
	at := 0
	for i, sp := range specs {
		n := sp.Count
		if n == 0 || i == len(specs)-1 && n > len(ranked)-at {
			n = len(ranked) - at
		}
		if at+n > len(ranked) {
			return fmt.Errorf("group %q has %d teams, the split needs more", from, len(ranked))
		}
		sg := SplitGroup{Name: sp.Name, Format: sp.Format, Carry: sp.Carry}
		for _, st := range ranked[at : at+n] {
			sg.Teams = append(sg.Teams, st.Team)
		}
		op.Groups = append(op.Groups, sg)
		at += n
	}
	return e.apply(op)
}

// Exchange moves the bottom n of `upper` down to `lower` and the top n of `lower` up: "after round 10,
// the last two of A go to B and the first two of B to A". The director decides what the moving teams
// bring (`movers`): their score as it is (CarryAll), a percentage of it (CarryAll, Fraction 0.5), or
// nothing (CarryNone), or head-to-head against their new group. The teams that stay keep theirs.
func (e *Event) Exchange(upper, lower string, n int, movers Carry) error {
	if err := e.allPlayed(); err != nil {
		return err
	}
	if e.Group(upper) == nil || e.Group(lower) == nil {
		return fmt.Errorf("no group %q or %q", upper, lower)
	}
	us, ls := e.Standings(upper), e.Standings(lower)
	if n < 1 || n > len(us) || n > len(ls) {
		return fmt.Errorf("cannot exchange %d teams", n)
	}
	op := Op{Kind: OpExchange, Group: upper, Lower: lower, Carry: movers}
	for _, st := range us[len(us)-n:] {
		op.Down = append(op.Down, st.Team)
	}
	for _, st := range ls[:n] {
		op.Up = append(op.Up, st.Team)
	}
	return e.apply(op)
}

// SetFormat changes the format a group plays from the next round (Swiss -> Danish).
func (e *Event) SetFormat(group string, f Format) error {
	return e.apply(Op{Kind: OpFormat, Group: group, Format: f})
}

func (e *Event) allPlayed() error {
	for _, m := range e.Matches {
		if m.Tied {
			return fmt.Errorf("round %d: %s - %s is tied and needs extra boards", m.Round, e.name(m.Home), e.name(m.Away))
		}
		if !m.Played {
			return fmt.Errorf("round %d: %s - %s is not played yet", m.Round, e.name(m.Home), e.name(m.Away))
		}
	}
	return nil
}

func (e *Event) name(id TeamID) string {
	if id == 0 {
		return "bye"
	}
	return e.Teams[id].Name
}

func vpOf(m Match, id TeamID) float64 {
	switch {
	case !m.Played:
		return 0
	case m.Home == id:
		return m.VPHome
	case m.Away == id:
		return m.VPAway
	}
	return 0
}

// carryOf is what team id brought into its group by membership ms, from the matches as they are now.
func (e *Event) carryOf(id TeamID, ms *membership) float64 {
	var v float64
	switch ms.rule.Kind {
	case CarryNone:
		return 0
	case CarryHeadToHead:
		peer := map[TeamID]bool{}
		for _, p := range ms.peers {
			peer[p] = p != id
		}
		for _, m := range e.Matches {
			if m.Round > ms.after || m.Bye() {
				continue
			}
			if (m.Home == id && peer[m.Away]) || (m.Away == id && peer[m.Home]) {
				v += vpOf(m, id)
			}
		}
	default:
		if ms.prev != nil {
			v = e.scoreIn(id, ms.prev, ms.after)
		} else {
			for _, m := range e.Matches {
				if m.Round <= ms.after {
					v += vpOf(m, id)
				}
			}
		}
	}
	return round2(v * ms.rule.factor())
}

// scoreIn is a team's group score under membership ms up to round upTo: its carry and the VPs since.
func (e *Event) scoreIn(id TeamID, ms *membership, upTo int) float64 {
	v := e.carryOf(id, ms)
	for _, m := range e.Matches {
		if m.Round > ms.after && m.Round <= upTo {
			v += vpOf(m, id)
		}
	}
	return v
}

// Record enters a match's IMPs; VPs follow from the event's scale. A knockout match has a winner
// instead: when it is level after carry-over, it is left Tied until AddPlayoff. Recording again
// corrects the result.
func (e *Event) Record(round int, home, away TeamID, impsHome, impsAway float64) error {
	return e.apply(Op{Kind: OpRecord, Round: round, Home: home, Away: away, IMPsHome: impsHome, IMPsAway: impsAway})
}

// SetCarry is the director's knockout carry-over for a match, in IMPs, set before or after its result.
func (e *Event) SetCarry(round int, home, away TeamID, carryHome, carryAway float64) error {
	return e.apply(Op{Kind: OpCarry, Round: round, Home: home, Away: away, IMPsHome: carryHome, IMPsAway: carryAway})
}

// AddPlayoff adds extra boards to a tied knockout match (usually 1 or 2 at a time); if it is still
// level it stays Tied for more.
func (e *Event) AddPlayoff(round int, home, away TeamID, boards int, impsHome, impsAway float64) error {
	return e.apply(Op{Kind: OpPlayoff, Round: round, Home: home, Away: away, Boards: boards, IMPsHome: impsHome, IMPsAway: impsAway})
}

func (e *Event) match(round int, home, away TeamID) (*Match, error) {
	for i := range e.Matches {
		if m := &e.Matches[i]; m.Round == round && m.Home == home && m.Away == away {
			return m, nil
		}
	}
	return nil, fmt.Errorf("round %d has no match %s - %s", round, e.name(home), e.name(away))
}

// Standing is a team's line in its group's table.
type Standing struct {
	Team               TeamID
	Rank               int
	VP                 float64 // carry + VPs since joining
	Carry              float64
	IMPsFor, IMPsAgain float64
	Played             int
}

// Standings ranks a group: VP; among teams still level, the VPs of their matches against each
// other; then the IMP quotient; then the seed.
func (e *Event) Standings(group string) []Standing {
	g := e.Group(group)
	if g == nil {
		return nil
	}
	by := map[TeamID]*Standing{}
	for id, ms := range g.members {
		c := e.carryOf(id, ms)
		by[id] = &Standing{Team: id, VP: c, Carry: c}
	}
	for _, m := range e.Matches {
		if !m.Played {
			continue
		}
		for _, side := range []struct {
			id       TeamID
			vp, f, a float64
		}{{m.Home, m.VPHome, m.IMPsHome, m.IMPsAway}, {m.Away, m.VPAway, m.IMPsAway, m.IMPsHome}} {
			st := by[side.id]
			if st == nil || m.Round <= g.members[side.id].after {
				continue
			}
			st.VP += side.vp
			st.IMPsFor += side.f
			st.IMPsAgain += side.a
			st.Played++
		}
	}
	var out []Standing
	for _, st := range by {
		st.VP = round2(st.VP)
		out = append(out, *st)
	}
	quot := func(s Standing) float64 {
		if s.IMPsAgain == 0 {
			return math.Inf(1)
		}
		return s.IMPsFor / s.IMPsAgain
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.VP != b.VP {
			return a.VP > b.VP
		}
		if qa, qb := quot(a), quot(b); qa != qb {
			return qa > qb
		}
		return e.Teams[a.Team].Seed < e.Teams[b.Team].Seed
	})
	e.headToHead(out)
	for i := range out {
		out[i].Rank = i + 1
		if i > 0 && out[i].VP == out[i-1].VP && out[i].IMPsFor == out[i-1].IMPsFor && out[i].IMPsAgain == out[i-1].IMPsAgain {
			out[i].Rank = out[i-1].Rank
		}
	}
	return out
}

// headToHead reorders each run of teams level on VP by the VPs of the matches among them.
func (e *Event) headToHead(out []Standing) {
	for i := 0; i < len(out); {
		j := i + 1
		for j < len(out) && out[j].VP == out[i].VP {
			j++
		}
		if j-i > 1 {
			tied := map[TeamID]bool{}
			for _, s := range out[i:j] {
				tied[s.Team] = true
			}
			h2h := map[TeamID]float64{}
			for _, m := range e.Matches {
				if m.Played && tied[m.Home] && tied[m.Away] {
					h2h[m.Home] += m.VPHome
					h2h[m.Away] += m.VPAway
				}
			}
			sort.SliceStable(out[i:j], func(a, b int) bool { return h2h[out[i+a].Team] > h2h[out[i+b].Team] })
		}
		i = j
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
