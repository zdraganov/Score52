// Package usebio writes results as USEBIO 1.2 XML, the file bridge.bg takes next to .pbn
// (`../bridge.bg/next/src/Results/UsebioParser.php` is what reads it there).
//
// The input is plain export structs, filled from the engine by the caller, so this package depends on
// nothing but the contract notation.
package usebio

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"time"

	"bridge.bg/scorer/internal/score"
)

const Program = "BBF Scorer"

type Player struct {
	Name         string
	FederationID string // players.ibn on bridge.bg: the ELO is matched by it
}

// Pairs scoring of an event.
type Method string

const (
	MatchPoints Method = "MATCH_POINTS"
	CrossIMPs   Method = "CROSS_IMPS"
	Butler      Method = "BUTLER_IMPS"
)

type PairsEvent struct {
	Title   string
	Date    time.Time
	Version string // the scorer's version
	Method  Method
	Pairs   []Pair
	Boards  []Board
}

type Pair struct {
	Number    int
	Direction string // "NS" / "EW" for a two-winner Mitchell, "" otherwise
	Players   []Player
	Place     int
	Tied      bool    // the place is shared ("3=")
	Score     float64 // matchpoints or IMPs
	Percent   float64 // MP only
	Boards    int
}

type Board struct {
	Number int
	Lines  []Line
}

// Line is a traveller line. A board not played (an artificial adjusted score) has Played false and
// Adjusted describing it ("A60/40"); its NS/EW points are still filled in.
type Line struct {
	Round    int
	NS, EW   int
	Played   bool
	Adjusted string
	Contract score.Contract
	Declarer string // N, E, S, W
	Lead     string // e.g. "SA"
	Tricks   int
	Score    int // NS
	// MP: each side's percentage on the board (bridge.bg shows these); IMP: cross-IMP or Butler points
	NSPoints, EWPoints float64
}

type TeamsEvent struct {
	Title   string
	Date    time.Time
	Version string
	Teams   []Team
	Matches []Match
}

type Team struct {
	ID      int
	Name    string
	Group   string // "A", "B"... when the stage is played in groups; bridge.bg ranks group after group
	Players []Player
	Place   int
	Tied    bool
	VP      float64
}

// Match is a round's match; one still to be played (Played false) shows whom a team meets, without score.
type Match struct {
	Played                bool
	Round                 int
	Home, Away            int // team ids
	FirstBoard, LastBoard int
	IMPsHome, IMPsAway    float64
	VPHome, VPAway        float64
}

// --- XML shape ---

type xUsebio struct {
	XMLName xml.Name `xml:"USEBIO"`
	Version string   `xml:"Version,attr"`
	Event   xEvent   `xml:"EVENT"`
}

type xEvent struct {
	Type         string   `xml:"EVENT_TYPE,attr"`
	Description  string   `xml:"EVENT_DESCRIPTION"`
	ProgramName  string   `xml:"PROGRAM_NAME"`
	ProgramVer   string   `xml:"PROGRAM_VERSION,omitempty"`
	Date         string   `xml:"DATE,omitempty"`
	BoardScoring string   `xml:"BOARD_SCORING_METHOD,omitempty"`
	Participants xPart    `xml:"PARTICIPANTS"`
	Matches      []xMatch `xml:"MATCH,omitempty"`
	Boards       []xBoard `xml:"BOARD,omitempty"`
}

type xPart struct {
	Pairs []xPair `xml:"PAIR,omitempty"`
	Teams []xTeam `xml:"TEAM,omitempty"`
}

type xPlayer struct {
	Name string `xml:"PLAYER_NAME"`
	ID   string `xml:"NATIONAL_ID_NUMBER,omitempty"`
}

type xPair struct {
	Number    int       `xml:"PAIR_NUMBER"`
	Direction string    `xml:"DIRECTION,omitempty"`
	Players   []xPlayer `xml:"PLAYER"`
	Place     string    `xml:"PLACE"`
	Total     string    `xml:"TOTAL_SCORE"`
	Percent   string    `xml:"PERCENTAGE,omitempty"`
	Boards    int       `xml:"BOARDS_PLAYED"`
}

type xTeam struct {
	ID      int       `xml:"TEAM_ID,attr"`
	Name    string    `xml:"TEAM_NAME,attr"`
	Group   string    `xml:"GROUP,omitempty"`
	Players []xPlayer `xml:"PLAYER"`
	Place   string    `xml:"PLACE"`
	Total   string    `xml:"TOTAL_SCORE"`
}

type xMatch struct {
	Round      int    `xml:"ROUND_NUMBER"`
	Team       int    `xml:"TEAM"`
	Opposing   int    `xml:"OPPOSING_TEAM"`
	StartBoard int    `xml:"START_BOARD_NUMBER,omitempty"`
	EndBoard   int    `xml:"END_BOARD_NUMBER,omitempty"`
	IMPs       string `xml:"TEAM_IMPS,omitempty"`
	OppIMPs    string `xml:"OPPOSING_TEAM_IMPS,omitempty"`
	Score      string `xml:"TEAM_SCORE,omitempty"`
	OppScore   string `xml:"OPPOSING_TEAM_SCORE,omitempty"`
}

type xBoard struct {
	Number int     `xml:"BOARD_NUMBER"`
	Lines  []xLine `xml:"TRAVELLER_LINE"`
}

type xLine struct {
	Round    int    `xml:"ROUND,omitempty"`
	NS       int    `xml:"NS_PAIR_NUMBER"`
	EW       int    `xml:"EW_PAIR_NUMBER"`
	Contract string `xml:"CONTRACT"`
	PlayedBy string `xml:"PLAYED_BY,omitempty"`
	Lead     string `xml:"LEAD,omitempty"`
	Tricks   string `xml:"TRICKS,omitempty"`
	Score    string `xml:"SCORE,omitempty"`
	NSMP     string `xml:"NS_MATCH_POINTS,omitempty"`
	EWMP     string `xml:"EW_MATCH_POINTS,omitempty"`
	NSCross  string `xml:"NS_CROSS_IMP_POINTS,omitempty"`
	EWCross  string `xml:"EW_CROSS_IMP_POINTS,omitempty"`
	NSButler string `xml:"NS_BUTLER_POINTS,omitempty"`
	EWButler string `xml:"EW_BUTLER_POINTS,omitempty"`
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// imps as a scoring program shows them: 30, 12.5
func imps(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func place(p int, tied bool) string {
	if tied {
		return fmt.Sprintf("%d=", p)
	}
	return strconv.Itoa(p)
}

func players(ps []Player) []xPlayer {
	out := make([]xPlayer, len(ps))
	for i, p := range ps {
		out[i] = xPlayer{Name: p.Name, ID: p.FederationID}
	}
	return out
}

func date(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("02/01/2006")
}

// WritePairs writes a pairs event (one session, a final, or the whole event).
func WritePairs(w io.Writer, e PairsEvent) error {
	typ := "MP_PAIRS"
	if e.Method == CrossIMPs {
		typ = "CROSS_IMP"
	} else if e.Method == Butler {
		typ = "BUTLER_PAIRS"
	}
	x := xUsebio{Version: "1.2", Event: xEvent{
		Type: typ, Description: e.Title, ProgramName: Program, ProgramVer: e.Version, Date: date(e.Date),
		BoardScoring: string(e.Method),
	}}
	for _, p := range e.Pairs {
		xp := xPair{Number: p.Number, Direction: p.Direction, Players: players(p.Players), Place: place(p.Place, p.Tied),
			Total: num(p.Score), Boards: p.Boards}
		if e.Method == MatchPoints {
			xp.Percent = num(p.Percent)
		}
		x.Event.Participants.Pairs = append(x.Event.Participants.Pairs, xp)
	}
	for _, b := range e.Boards {
		xb := xBoard{Number: b.Number}
		for _, l := range b.Lines {
			xl := xLine{Round: l.Round, NS: l.NS, EW: l.EW}
			if l.Played {
				xl.Contract = l.Contract.String()
				if l.Contract.Level > 0 {
					xl.PlayedBy, xl.Lead, xl.Tricks = l.Declarer, l.Lead, strconv.Itoa(l.Tricks)
				}
				xl.Score = strconv.Itoa(l.Score)
			} else {
				xl.Contract = l.Adjusted
			}
			ns, ew := num(l.NSPoints), num(l.EWPoints)
			switch e.Method {
			case CrossIMPs:
				xl.NSCross, xl.EWCross = ns, ew
			case Butler:
				xl.NSButler, xl.EWButler = ns, ew
			default:
				xl.NSMP, xl.EWMP = ns, ew
			}
			xb.Lines = append(xb.Lines, xl)
		}
		x.Event.Boards = append(x.Event.Boards, xb)
	}
	return encode(w, x)
}

// WriteTeams writes a teams event (a stage, or one group of it). Each match is written once, from the
// home team's side.
func WriteTeams(w io.Writer, e TeamsEvent) error {
	x := xUsebio{Version: "1.2", Event: xEvent{
		Type: "SWISS_TEAMS", Description: e.Title, ProgramName: Program, ProgramVer: e.Version, Date: date(e.Date),
		BoardScoring: "IMPS",
	}}
	for _, t := range e.Teams {
		x.Event.Participants.Teams = append(x.Event.Participants.Teams, xTeam{ID: t.ID, Name: t.Name, Group: t.Group, Players: players(t.Players),
			Place: place(t.Place, t.Tied), Total: num(t.VP)})
	}
	for _, m := range e.Matches {
		xm := xMatch{Round: m.Round, Team: m.Home, Opposing: m.Away, StartBoard: m.FirstBoard, EndBoard: m.LastBoard}
		if m.Played {
			xm.IMPs, xm.OppIMPs = imps(m.IMPsHome), imps(m.IMPsAway)
			xm.Score, xm.OppScore = num(m.VPHome), num(m.VPAway)
		}
		x.Event.Matches = append(x.Event.Matches, xm)
	}
	return encode(w, x)
}

func encode(w io.Writer, x xUsebio) error {
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(x); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
