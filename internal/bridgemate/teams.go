package bridgemate

import (
	"fmt"
	"time"
)

// A teams round is one .bws, as Magic Contest makes it: section A is the open room, B the closed room,
// one table per match in each, and both tables of match k are table k with Tables.Group k. The next
// round is a new file (Bridgemate can't regroup tables within one), and BCS is restarted on it.
//
// Pair numbers are the teams' numbers: at A the home team sits NS and the away team EW, at B the other
// way round, so each section's numbers are unique and the Bridgemates show the teams' own numbers.

const (
	SectionOpen   = 1
	SectionClosed = 2
)

// TeamMatch is one match of the round.
type TeamMatch struct {
	Table      int // 1, 2...: the match's tables in both rooms
	Home, Away int // team numbers
	Boards     int // from the round's first board
	// players to show: N, S, E, W of the open room (home NS) and of the closed room (away NS)
	Open, Closed [4]Player
}

// TeamsSetup lays out one round of a teams event: each match plays boards first..first+Boards-1.
func TeamsSetup(name string, date time.Time, first int, matches []TeamMatch) (Setup, error) {
	if len(matches) == 0 {
		return Setup{}, fmt.Errorf("no matches")
	}
	if first < 1 {
		return Setup{}, fmt.Errorf("first board %d", first)
	}
	s := Setup{Session: SessionRow{ID: 1, Name: clip(name, 40), Date: date}}
	for _, sec := range []struct {
		id     int
		letter string
	}{{SectionOpen, "A"}, {SectionClosed, "B"}} {
		s.Sections = append(s.Sections, SectionRow{ID: sec.id, Letter: sec.letter, Tables: len(matches), Session: 1, Scoring: ScoringTeams, Winners: 1})
	}
	seen := map[int]bool{}
	for _, m := range matches {
		if m.Table < 1 || seen[m.Table] || m.Home == m.Away || m.Home < 1 || m.Away < 1 || m.Boards < 1 {
			return Setup{}, fmt.Errorf("match at table %d: %d - %d", m.Table, m.Home, m.Away)
		}
		seen[m.Table] = true
		for _, room := range []struct {
			sec     int
			ns, ew  int
			players [4]Player
		}{{SectionOpen, m.Home, m.Away, m.Open}, {SectionClosed, m.Away, m.Home, m.Closed}} {
			s.Tables = append(s.Tables, TableRow{Section: room.sec, Table: m.Table, Group: m.Table})
			s.Rounds = append(s.Rounds, RoundRow{Section: room.sec, Table: m.Table, Round: 1, NS: room.ns, EW: room.ew,
				LowBoard: first, HighBoard: first + m.Boards - 1})
			for i, dir := range []string{"N", "S", "E", "W"} {
				if p := room.players[i]; p.Name != "" || p.Number != "" {
					s.Players = append(s.Players, PlayerRow{Section: room.sec, Table: m.Table, Direction: dir, Number: p.Number, Name: clip(p.Name, 18)})
				}
			}
		}
	}
	return s, s.Check()
}
