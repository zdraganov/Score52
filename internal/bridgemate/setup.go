// Package bridgemate exchanges a session with Bridgemate Control Software (BCS) through its .bws
// database: the movement and names go in before play, results come back during it.
// See docs/bridgemate.md for the format and what was checked against BCS.
package bridgemate

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zdraganov/Score52/internal/movement"
	"github.com/zdraganov/Score52/internal/pairs"
)

// ScoringType is Section.ScoringType.
type ScoringType int

const (
	ScoringMP       ScoringType = 1
	ScoringButler   ScoringType = 2
	ScoringCrossIMP ScoringType = 3
	ScoringTeams    ScoringType = 4
)

// ScoringFor maps a pairs scoring method to BCS's.
func ScoringFor(m pairs.Method) ScoringType {
	switch m {
	case pairs.MethodButler:
		return ScoringButler
	case pairs.MethodCrossIMP:
		return ScoringCrossIMP
	}
	return ScoringMP
}

type SessionRow struct {
	ID   int
	Name string // ≤40
	Date time.Time
	GUID string // keep it when rewriting the session
}

type SectionRow struct {
	ID          int
	Letter      string
	Tables      int
	MissingPair int
	Session     int
	Scoring     ScoringType
	Winners     int // 1 one field, 2 a two-winner Mitchell
}

type TableRow struct {
	Section, Table int
	Group          int
}

type RoundRow struct {
	Section, Table, Round int
	NS, EW                int
	LowBoard, HighBoard   int
}

type PlayerRow struct {
	Section, Table int
	Direction      string // N S E W
	Number         string // federation number
	Name           string // ≤18
}

// Setup is everything Score52 writes into a .bws before play.
type Setup struct {
	Session  SessionRow
	Sections []SectionRow
	Tables   []TableRow
	Rounds   []RoundRow
	Players  []PlayerRow
	Names    []NameRow // PlayerNames: the numbers BCS looks names up by
}

// NameRow is a row of PlayerNames: a federation number (ID when it is a number) and its name.
type NameRow struct {
	ID    int
	StrID string // ≤18
	Name  string // ≤18
}

// Player is a name to show on the Bridgemates.
type Player struct {
	Number string
	Name   string
}

// PairsSection describes one pairs section of a session: its movement and who plays at each
// movement pair number (players[n] = the two players of pair n; missing entries show no names).
type PairsSection struct {
	Letter         string
	Movement       movement.Movement
	BoardsPerRound int
	FirstBoard     int
	Scoring        pairs.Method
	Players        map[int][2]Player
}

// PairsSetup lays out a pairs session. Section ids are 1, 2... in order; table numbers are the
// movement's. Pair numbers are unique within a section in all our movements, so a phantom is
// Section.MissingPair and BCS skips its rounds.
func PairsSetup(name string, date time.Time, sections ...PairsSection) (Setup, error) {
	s := Setup{Session: SessionRow{ID: 1, Name: clip(name, 40), Date: date}}
	for i, ps := range sections {
		m := ps.Movement
		if err := m.Validate(); err != nil {
			return Setup{}, fmt.Errorf("section %s: %w", ps.Letter, err)
		}
		whole := movement.SectionInfo{Letter: ps.Letter, FirstTable: 1, Tables: m.Tables, FirstPair: 1, Pairs: m.Pairs}
		s.addSection(i+1, m, whole, ps.BoardsPerRound, ps.FirstBoard, ps.Scoring, ps.Players)
	}
	return s, s.Check()
}

// SessionSetup lays out a Score52 session as it is stored: one movement, maybe played in sections
// (movement.Combine). Section k is BCS section k+1, its tables and pairs numbered from 1 as the
// players see them ("B3" is table 3 of section B); players are keyed by the movement's pair number.
func SessionSetup(name string, date time.Time, m movement.Movement, bpr, first int, method pairs.Method, players map[int][2]Player) (Setup, error) {
	s := Setup{Session: SessionRow{ID: 1, Name: clip(name, 40), Date: date}}
	if err := m.Validate(); err != nil {
		return Setup{}, err
	}
	for k, x := range SessionSections(m) {
		s.addSection(k+1, m, x, bpr, first, method, players)
	}
	return s, s.Check()
}

// SessionSections are the parts of a movement BCS sees as sections: its own sections, or all of it as A.
func SessionSections(m movement.Movement) []movement.SectionInfo {
	if len(m.Sections) > 0 {
		return m.Sections
	}
	return []movement.SectionInfo{{Letter: "A", FirstTable: 1, Tables: m.Tables, FirstPair: 1, Pairs: m.Pairs, Kind: m.Kind}}
}

// addSection writes the tables x covers of movement m as section id, renumbered from 1.
func (s *Setup) addSection(id int, m movement.Movement, x movement.SectionInfo, bpr, first int, method pairs.Method, players map[int][2]Player) {
	if first == 0 {
		first = 1
	}
	local := func(p int) int {
		if p == 0 {
			return 0
		}
		return p - x.FirstPair + 1
	}
	missing := 0
	for _, ph := range append([]int{m.Phantom}, m.Phantoms...) {
		if ph != 0 && ph >= x.FirstPair && ph < x.FirstPair+x.Pairs {
			missing = local(ph)
		}
	}
	winners := 1
	if m.TwoFields {
		winners = 2
	}
	s.Sections = append(s.Sections, SectionRow{ID: id, Letter: x.Letter, Tables: x.Tables, MissingPair: missing,
		Session: 1, Scoring: ScoringFor(method), Winners: winners})
	seat := map[[2]int]movement.Seat{}
	for _, st := range m.Seats {
		seat[[2]int{st.Table, st.Round}] = st
	}
	for t := 1; t <= x.Tables; t++ {
		s.Tables = append(s.Tables, TableRow{Section: id, Table: t})
		for r := 1; r <= m.Rounds; r++ {
			st, ok := seat[[2]int{x.FirstTable + t - 1, r}]
			row := RoundRow{Section: id, Table: t, Round: r}
			if ok { // a table the movement leaves empty keeps all zeros
				row.NS, row.EW = local(st.NS), local(st.EW)
				row.LowBoard = first + (st.Set-1)*bpr
				row.HighBoard = row.LowBoard + bpr - 1
			}
			s.Rounds = append(s.Rounds, row)
		}
	}
	// names at the pairs' first-round tables (round 0 = before play)
	for _, st := range m.Seats {
		if st.Round != 1 || st.Table < x.FirstTable || st.Table >= x.FirstTable+x.Tables {
			continue
		}
		for _, side := range []struct {
			pair int
			dirs [2]string
		}{{st.NS, [2]string{"N", "S"}}, {st.EW, [2]string{"E", "W"}}} {
			pl, ok := players[side.pair]
			if !ok || m.IsPhantom(side.pair) {
				continue
			}
			for k, p := range pl {
				if p.Number == "" && p.Name == "" {
					continue
				}
				s.Players = append(s.Players, PlayerRow{Section: id, Table: st.Table - x.FirstTable + 1, Direction: side.dirs[k], Number: p.Number, Name: clip(p.Name, 18)})
			}
		}
	}
}

// AddNames puts players into PlayerNames, so a Bridgemate shows the name of whoever enters their
// federation number at any table. Players without a number are left out; a number appears once.
func (s *Setup) AddNames(players ...Player) {
	seen := map[string]bool{}
	for _, n := range s.Names {
		seen[n.StrID] = true
	}
	for _, p := range players {
		id := strings.TrimSpace(p.Number)
		if id == "" || p.Name == "" || seen[id] || utf8.RuneCountInString(id) > 18 {
			continue
		}
		seen[id] = true
		n := NameRow{StrID: id, Name: clip(p.Name, 18)}
		if v, err := strconv.Atoi(id); err == nil && v > 0 {
			n.ID = v
		}
		s.Names = append(s.Names, n)
	}
}

// Check holds the setup to the Bridgemate II/III limits.
func (s Setup) Check() error {
	if len(s.Sections) == 0 || len(s.Sections) > 78 {
		return fmt.Errorf("a session has 1..78 sections, not %d", len(s.Sections))
	}
	if len(s.Tables) > 256 {
		return fmt.Errorf("%d tables: one Bridgemate server takes at most 256", len(s.Tables))
	}
	type key struct{ s, t int }
	rounds, boards := map[key]int{}, map[key]int{}
	for _, t := range s.Tables {
		if t.Table < 1 || t.Table > 4095 {
			return fmt.Errorf("table number %d: Bridgemate takes 1..4095", t.Table)
		}
	}
	for _, r := range s.Rounds {
		if r.Round < 1 || r.Round > 255 {
			return fmt.Errorf("round %d: Bridgemate takes 1..255", r.Round)
		}
		if r.NS > 4095 || r.EW > 4095 {
			return fmt.Errorf("round %d table %d: pair numbers go up to 4095", r.Round, r.Table)
		}
		if r.HighBoard > 127 {
			return fmt.Errorf("board %d: Bridgemate's highest board number is 127", r.HighBoard)
		}
		if n := r.HighBoard - r.LowBoard + 1; r.LowBoard > 0 && n > 54 {
			return fmt.Errorf("%d boards in a round: at most 54", n)
		}
		k := key{r.Section, r.Table}
		rounds[k]++
		if r.LowBoard > 0 {
			boards[k] += r.HighBoard - r.LowBoard + 1
		}
	}
	for k, n := range rounds {
		if n+boards[k] > 256 {
			return fmt.Errorf("table %d: %d rounds + %d boards is over Bridgemate's 256", k.t, n, boards[k])
		}
	}
	return nil
}

// clip cuts a string to n characters (not bytes: names are Cyrillic).
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}
