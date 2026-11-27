//go:build windows

// bwstool tries the BridgeMate code against a real .bws on Windows (build with GOARCH=386).
//
//	bwstool create <template.bws> <out.bws>   a 5-table Mitchell, 3 boards a round, with names
//	bwstool read <file.bws>                   the results it holds, scored
package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/zdraganov/Score52/internal/bridgemate"
	"github.com/zdraganov/Score52/internal/movement"
	"github.com/zdraganov/Score52/internal/pairs"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: bwstool create <template.bws> <out.bws> | read <file.bws>")
		os.Exit(2)
	}
	m, err := movement.NewMitchell(5, 5, movement.MitchellOptions{})
	check(err)
	switch os.Args[1] {
	case "create":
		check(bridgemate.Create(os.Args[2], os.Args[3]))
		db, err := bridgemate.Open(os.Args[3])
		check(err)
		defer db.Close()
		players := map[int][2]bridgemate.Player{}
		for p := 1; p <= 10; p++ {
			players[p] = [2]bridgemate.Player{
				{Number: fmt.Sprint(1000 + 2*p), Name: fmt.Sprintf("Играч %d Север", p)},
				{Number: fmt.Sprint(1001 + 2*p), Name: fmt.Sprintf("Играч %d Юг", p)},
			}
		}
		s, err := bridgemate.PairsSetup("Тест на Score52", time.Now(), bridgemate.PairsSection{
			Letter: "A", Movement: m, BoardsPerRound: 3, Scoring: pairs.MethodMP, Players: players})
		check(err)
		pc, err := bridgemate.ComputerName()
		check(err)
		check(bridgemate.Write(db, s, pc))
		fmt.Printf("written: %d tables, %d rounds, %d players, client %q\n", len(s.Tables), len(s.Rounds), len(s.Players), pc)
	case "read":
		db, err := bridgemate.Open(os.Args[2])
		check(err)
		defer db.Close()
		rows, err := bridgemate.ReadReceived(db)
		check(err)
		boards, errs := bridgemate.Current(rows)
		for _, e := range errs {
			fmt.Println("UNREADABLE:", e)
		}
		var lines []pairs.Line
		for _, b := range boards {
			fmt.Printf("board %2d table %d: %d-%d %-8s %3s  NS %5d\n", b.Received.Board, b.Table, b.PairNS, b.PairEW, b.Contract, b.Result, b.ScoreNS)
			if l, ok := b.Line(); ok {
				lines = append(lines, l)
			}
		}
		res, err := pairs.ScoreSession(m, 3, 1, pairs.MethodMP, 0, lines)
		check(err)
		var nums []int
		for p := range res.Totals {
			nums = append(nums, p)
		}
		sort.Ints(nums)
		fmt.Printf("%d rows, %d current results\n", len(rows), len(boards))
		for _, p := range nums {
			t := res.Totals[p]
			fmt.Printf("pair %2d: %5.1f / %5.1f MP = %5.2f%% over %d boards\n", p, t.Score, t.Top, 100*t.Score/t.Top, t.Boards)
		}
	}
}

func check(err error) {
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
}
