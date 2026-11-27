// Package score holds the arithmetic of duplicate bridge: the score of a contract, IMPs, matchpoints,
// cross-IMPs and Butler, VP conversion and board-a-match. It knows nothing about movements or events.
package score

import (
	"fmt"
	"strings"
)

// Strain is the denomination of a contract.
type Strain int

const (
	Clubs Strain = iota
	Diamonds
	Hearts
	Spades
	NoTrump
)

// Doubled is the state of a contract: undoubled, doubled or redoubled.
type Doubled int

const (
	Undoubled Doubled = iota
	Double
	Redouble
)

// Contract is a final contract. Level 0 means the board was passed out.
type Contract struct {
	Level   int
	Strain  Strain
	Doubled Doubled
}

// PassOut is the contract of a passed-out board.
var PassOut = Contract{}

// ParseContract reads "4S", "3NTX", "6Hxx", "7CXX", "PASS".
func ParseContract(s string) (Contract, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "PASS" || s == "P" || s == "AP" {
		return PassOut, nil
	}
	if len(s) < 2 || s[0] < '1' || s[0] > '7' {
		return Contract{}, fmt.Errorf("contract %q: bad level", s)
	}
	c := Contract{Level: int(s[0] - '0')}
	rest := s[1:]
	switch {
	case strings.HasPrefix(rest, "NT"):
		c.Strain, rest = NoTrump, rest[2:]
	case strings.HasPrefix(rest, "N"):
		c.Strain, rest = NoTrump, rest[1:]
	case rest[0] == 'S':
		c.Strain, rest = Spades, rest[1:]
	case rest[0] == 'H':
		c.Strain, rest = Hearts, rest[1:]
	case rest[0] == 'D':
		c.Strain, rest = Diamonds, rest[1:]
	case rest[0] == 'C':
		c.Strain, rest = Clubs, rest[1:]
	default:
		return Contract{}, fmt.Errorf("contract %q: bad strain", s)
	}
	switch rest {
	case "":
	case "X":
		c.Doubled = Double
	case "XX":
		c.Doubled = Redouble
	default:
		return Contract{}, fmt.Errorf("contract %q: bad doubling", s)
	}
	return c, nil
}

func (c Contract) String() string {
	if c.Level == 0 {
		return "PASS"
	}
	return fmt.Sprintf("%d%s%s", c.Level, [...]string{"C", "D", "H", "S", "NT"}[c.Strain], [...]string{"", "X", "XX"}[c.Doubled])
}

// Score is the declaring side's score for making tricks (0-13) in contract c, vulnerable or not.
// A passed-out board scores 0.
func Score(c Contract, vul bool, tricks int) int {
	if c.Level == 0 {
		return 0
	}
	mult := [...]int{1, 2, 4}[c.Doubled]
	need := c.Level + 6
	if tricks < need {
		return -undertricks(need-tricks, vul, c.Doubled)
	}

	// Contracted tricks
	per := 20
	if c.Strain >= Hearts {
		per = 30
	}
	trickScore := per * c.Level
	if c.Strain == NoTrump {
		trickScore += 10
	}
	trickScore *= mult

	total := trickScore
	if trickScore >= 100 {
		total += pick(vul, 500, 300)
	} else {
		total += 50
	}
	switch c.Level {
	case 6:
		total += pick(vul, 750, 500)
	case 7:
		total += pick(vul, 1500, 1000)
	}
	total += [...]int{0, 50, 100}[c.Doubled]

	over := tricks - need
	switch c.Doubled {
	case Undoubled:
		total += over * per
	case Double:
		total += over * pick(vul, 200, 100)
	case Redouble:
		total += over * pick(vul, 400, 200)
	}
	return total
}

func undertricks(n int, vul bool, d Doubled) int {
	if d == Undoubled {
		return n * pick(vul, 100, 50)
	}
	var s int
	for i := 1; i <= n; i++ {
		switch {
		case vul && i == 1:
			s += 200
		case vul:
			s += 300
		case i == 1:
			s += 100
		case i <= 3:
			s += 200
		default:
			s += 300
		}
	}
	if d == Redouble {
		s *= 2
	}
	return s
}

func pick(vul bool, v, nv int) int {
	if vul {
		return v
	}
	return nv
}

// Vulnerability of a board by the standard 16-board cycle.
type Vulnerability int

const (
	None Vulnerability = iota
	NS
	EW
	Both
)

// BoardVulnerability is the standard vulnerability of board n (1-based).
func BoardVulnerability(n int) Vulnerability {
	return [...]Vulnerability{None, NS, EW, Both, NS, EW, Both, None, EW, Both, None, NS, Both, None, NS, EW}[(n-1)%16]
}

// Dealer of board n: 0 = N, 1 = E, 2 = S, 3 = W.
func BoardDealer(n int) int { return (n - 1) % 4 }

// Vulnerable says whether the NS (ns=true) or EW side is vulnerable.
func (v Vulnerability) Vulnerable(ns bool) bool {
	return v == Both || (ns && v == NS) || (!ns && v == EW)
}

// NSScore is the score from NS's point of view for a contract declared by NS (declNS) or EW.
func NSScore(n int, c Contract, declNS bool, tricks int) int {
	s := Score(c, BoardVulnerability(n).Vulnerable(declNS), tricks)
	if declNS {
		return s
	}
	return -s
}
