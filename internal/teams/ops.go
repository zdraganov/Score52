package teams

import (
	"fmt"
	"slices"

	"bridge.bg/scorer/internal/score"
)

type OpKind string

const (
	OpStart    OpKind = "start"
	OpPair     OpKind = "pair"
	OpRecord   OpKind = "record"
	OpCarry    OpKind = "carry"
	OpPlayoff  OpKind = "playoff"
	OpSplit    OpKind = "split"
	OpExchange OpKind = "exchange"
	OpFormat   OpKind = "format"
)

// SplitGroup is one group a split made, with the teams that went into it.
type SplitGroup struct {
	Name   string   `json:"name"`
	Format Format   `json:"format"`
	Carry  Carry    `json:"carry"`
	Teams  []TeamID `json:"teams"`
}

// Op is one change to an event, as stored. Which fields are used depends on Kind.
type Op struct {
	Kind OpKind `json:"kind"`
	// start
	Teams  []Team  `json:"teams,omitempty"`
	Scale  string  `json:"scale,omitempty"`
	ByeVP  float64 `json:"bye_vp,omitempty"`
	Format Format  `json:"format,omitempty"` // start, format
	// pair: the round's matches; round robin groups' schedule order when they start one
	Matches []Match             `json:"matches,omitempty"`
	RROrder map[string][]TeamID `json:"rr_order,omitempty"`
	// record, carry (IMPs = the carry), playoff
	Round    int     `json:"round,omitempty"`
	Home     TeamID  `json:"home,omitempty"`
	Away     TeamID  `json:"away,omitempty"`
	Boards   int     `json:"boards,omitempty"`
	IMPsHome float64 `json:"imps_home,omitempty"`
	IMPsAway float64 `json:"imps_away,omitempty"`
	// split (Group = source), exchange (Group = upper), format
	Group  string       `json:"group,omitempty"`
	Groups []SplitGroup `json:"groups,omitempty"`
	Lower  string       `json:"lower,omitempty"`
	Down   []TeamID     `json:"down,omitempty"`
	Up     []TeamID     `json:"up,omitempty"`
	Carry  Carry        `json:"carry,omitempty"`
}

// apply is the only place an event changes; a successful op goes into the log.
func (e *Event) apply(op Op) error {
	var err error
	switch op.Kind {
	case OpStart:
		err = e.start(op)
	case OpPair:
		err = e.applyPair(op)
	case OpRecord:
		err = e.applyRecord(op)
	case OpCarry:
		err = e.applyCarry(op)
	case OpPlayoff:
		err = e.applyPlayoff(op)
	case OpSplit:
		err = e.applySplit(op)
	case OpExchange:
		err = e.applyExchange(op)
	case OpFormat:
		g := e.Group(op.Group)
		if g == nil {
			return fmt.Errorf("no group %q", op.Group)
		}
		g.Format = op.Format
	default:
		err = fmt.Errorf("unknown op %q", op.Kind)
	}
	if err == nil {
		e.Log = append(e.Log, op)
	}
	return err
}

func (e *Event) start(op Op) error {
	if e.Teams != nil {
		return fmt.Errorf("the event has started")
	}
	s, ok := Scales[op.Scale]
	if !ok {
		return fmt.Errorf("unknown VP scale %q", op.Scale)
	}
	e.Teams, e.ScaleName, e.Scale, e.ByeVP = map[TeamID]Team{}, op.Scale, s, op.ByeVP
	g := &Group{Name: "", Format: op.Format, Carry: Carry{Kind: CarryAll}, members: map[TeamID]*membership{}}
	for _, t := range op.Teams {
		if t.ID <= 0 || e.Teams[t.ID].ID != 0 {
			return fmt.Errorf("bad or repeated team id %d", t.ID)
		}
		e.Teams[t.ID] = t
		g.members[t.ID] = &membership{rule: Carry{Kind: CarryAll}}
	}
	e.Groups = []*Group{g}
	return nil
}

func (e *Event) applyPair(op Op) error {
	if err := e.allPlayed(); err != nil {
		return err
	}
	round := e.Round + 1
	for _, m := range op.Matches {
		g := e.Group(m.Group)
		if m.Round != round || g == nil || g.members[m.Home] == nil || (m.Away != 0 && g.members[m.Away] == nil) {
			return fmt.Errorf("match %s - %s does not belong to round %d of group %q", e.name(m.Home), e.name(m.Away), round, m.Group)
		}
	}
	for name, order := range op.RROrder {
		if g := e.Group(name); g != nil && g.order == nil {
			g.order = order
		}
	}
	for _, g := range e.Groups {
		if g.Format == RoundRobin && slices.ContainsFunc(op.Matches, func(m Match) bool { return m.Group == g.Name }) {
			g.rrRound++
		}
	}
	e.Round = round
	for _, m := range op.Matches {
		m.Knockout = e.Group(m.Group).Format == Knockout
		if m.Bye() {
			m.Played = true
			if !m.Knockout {
				m.VPHome = e.ByeVP
			}
		}
		e.Matches = append(e.Matches, m)
	}
	return nil
}

func (e *Event) applyRecord(op Op) error {
	m, err := e.match(op.Round, op.Home, op.Away)
	if err != nil {
		return err
	}
	if m.Bye() {
		return fmt.Errorf("a bye has no result")
	}
	m.IMPsHome, m.IMPsAway, m.Played = op.IMPsHome, op.IMPsAway, true
	if !m.Knockout {
		m.VPHome, m.VPAway = score.MatchVP(e.Scale, op.IMPsHome-op.IMPsAway, m.Boards)
		return nil
	}
	h, a := m.KOTotal()
	m.Tied = h == a
	return nil
}

func (e *Event) applyCarry(op Op) error {
	m, err := e.match(op.Round, op.Home, op.Away)
	if err != nil {
		return err
	}
	if !m.Knockout {
		return fmt.Errorf("%s - %s is not a knockout match", e.name(op.Home), e.name(op.Away))
	}
	m.CarryHome, m.CarryAway = op.IMPsHome, op.IMPsAway
	if m.Played {
		h, a := m.KOTotal()
		m.Tied = h == a
	}
	return nil
}

func (e *Event) applyPlayoff(op Op) error {
	m, err := e.match(op.Round, op.Home, op.Away)
	if err != nil {
		return err
	}
	if !m.Tied {
		return fmt.Errorf("%s - %s is not tied", e.name(op.Home), e.name(op.Away))
	}
	m.PlayoffBoards += op.Boards
	m.PlayoffHome += op.IMPsHome
	m.PlayoffAway += op.IMPsAway
	h, a := m.KOTotal()
	m.Tied = h == a
	return nil
}

func (e *Event) applySplit(op Op) error {
	if err := e.allPlayed(); err != nil {
		return err
	}
	src := e.Group(op.Group)
	if src == nil {
		return fmt.Errorf("no group %q", op.Group)
	}
	seen := map[TeamID]bool{}
	var made []*Group
	for _, sg := range op.Groups {
		if sg.Name != op.Group && e.Group(sg.Name) != nil {
			return fmt.Errorf("there is a group %q already", sg.Name)
		}
		g := &Group{Name: sg.Name, Format: sg.Format, Carry: sg.Carry, since: e.Round, members: map[TeamID]*membership{}}
		for _, id := range sg.Teams {
			if src.members[id] == nil || seen[id] {
				return fmt.Errorf("team %s is not in group %q, or is put in two groups", e.name(id), op.Group)
			}
			seen[id] = true
			g.members[id] = &membership{after: e.Round, rule: sg.Carry, peers: sg.Teams}
		}
		made = append(made, g)
	}
	if len(seen) != len(src.members) {
		return fmt.Errorf("the split leaves %d teams of %q without a group", len(src.members)-len(seen), op.Group)
	}
	var groups []*Group
	for _, g := range e.Groups {
		if g == src {
			groups = append(groups, made...)
		} else {
			groups = append(groups, g)
		}
	}
	e.Groups = groups
	return nil
}

func (e *Event) applyExchange(op Op) error {
	if err := e.allPlayed(); err != nil {
		return err
	}
	u, l := e.Group(op.Group), e.Group(op.Lower)
	if u == nil || l == nil {
		return fmt.Errorf("no group %q or %q", op.Group, op.Lower)
	}
	move := func(ids []TeamID, from, to *Group) error {
		for _, id := range ids {
			if from.members[id] == nil {
				return fmt.Errorf("team %s is not in group %q", e.name(id), from.Name)
			}
		}
		return nil
	}
	if err := move(op.Down, u, l); err != nil {
		return err
	}
	if err := move(op.Up, l, u); err != nil {
		return err
	}
	prev := map[TeamID]*membership{}
	for _, id := range op.Down {
		prev[id] = u.members[id]
		delete(u.members, id)
	}
	for _, id := range op.Up {
		prev[id] = l.members[id]
		delete(l.members, id)
	}
	join := func(ids []TeamID, g *Group) {
		var peers []TeamID
		for id := range g.members {
			peers = append(peers, id)
		}
		peers = append(peers, ids...)
		for _, id := range ids {
			g.members[id] = &membership{after: e.Round, rule: op.Carry, peers: peers, prev: prev[id]}
		}
		g.order, g.rrRound = nil, 0
	}
	join(op.Down, l)
	join(op.Up, u)
	return nil
}
