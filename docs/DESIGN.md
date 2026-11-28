# Score52 — design

Scoring and movement program for the Bulgarian Bridge Federation: pairs (MP and IMP) and teams
(IMP → VP, BAM), multi-session and multi-stage, offline at the venue, BridgeMate for result entry,
results straight to bridge.bg.

## Shape

One Go binary, as `../bridge.bg/tools/results-sync` is: it serves its UI on `127.0.0.1` and opens the
browser. The same binary runs in two modes:

| | Venue (default) | Hosted |
|---|---|---|
| Where | the director's Windows laptop | a server, behind https |
| Data | SQLite file per event, next to the program | the same SQLite files |
| Network | none needed; bridge.bg when there is one | — |
| BridgeMate | yes (Windows: ODBC to the `.bws`) | no |

Offline-first means the venue copy is the master: everything works with the cable pulled out, and
talking to bridge.bg (entries in, results out) is a queue that drains when the network comes back.
The hosted mode is for following an event remotely and for directors without BridgeMate (results
typed in from travellers or entered by players on phones), not a second source of truth.

Build like results-sync: `CGO_ENABLED=0`, cross-compiled from macOS, one `.exe`. That needs a pure-Go
SQLite (`modernc.org/sqlite`) and a pure-Go ODBC binding on Windows; those are the only planned
dependencies.

```
cmd/score52/         main: flags, mode, opens the browser
internal/score/      contract score, IMP, matchpoints (Neuberg), cross-IMP, Butler, VP, BAM    [done]
internal/movement/   Mitchell, skip Mitchell, Howell (shared boards), duplicated round robin,
                     validation, choosing a movement for a number of pairs and boards           [done]
internal/pairs/      session scoring (movement + travellers -> board points, pair totals),
                     next-session seating (mix the fields, no repeats), stage totals,
                     qualification into finals with carry-over                                   [done]
internal/teams/      groups, split / close / exchange, carry-over, Swiss / Danish /
                     round robin / knockout pairing, standings and tie-breaks                    [done]
internal/store/      one SQLite file per event: entries, sessions, results with history, teams log [done]
internal/bridgemate/ BCS .bws: write sections, tables, rounds, player numbers; read results
internal/usebio/     USEBIO 1.2 export (pairs MP / cross-IMP / Butler, teams), checked against the
                     site's own UsebioParser.php in the tests                                    [done]
internal/pbn/        PBN export, only if needed (see Results files)
internal/bridgebg/   td_ token connect, entries in, results to the session/final/stage slots
web/                 the UI (embedded with go:embed)
```

The engine packages are plain functions over plain data with no I/O, so they are tested in isolation and
can be driven by the UI, by an import, or by a test that replays a real past event.

## Storage

One SQLite file per event (`modernc.org/sqlite`, pure Go, so the `.exe` still builds with
`CGO_ENABLED=0`). A backup is a copy of the file. WAL mode, foreign keys on, one connection.

- **Schema migrations** are numbered SQL in `internal/store/schema.go`, tracked by `PRAGMA user_version`.
  A file from a newer program is refused rather than half-read. A shipped migration is never edited.
- **Pairs**: `players`, `entries` (a pair or team) with `entry_players`, `sessions` (slot `s1`/`f1`,
  scoring, the movement and the seating as JSON), `results` (traveller lines by movement pair
  numbers, as BridgeMate reports them; `source` manual/bridgemate). Triggers copy every changed or
  deleted line into `result_history`, so the director can see what a correction replaced.
- **Teams**: the event is its operation log (`team_ops`, one `teams.Op` per row) and is rebuilt by
  replaying it. Ops hold decisions as they were made: the round's pairings, which teams went to which
  group at a split or exchange. So a corrected score never re-decides a pairing or a group. Carry-overs
  and standings are worked out from the matches every time, so the same correction does reach every
  group's carry-over after it. The log only grows; a file with more ops than the event in memory is refused.
- Event settings (kind, title, date, bridge.bg tournament id, the pairs stage plan) are rows in `meta`.

## Pairs

- **Movement choice** (`movement.Options`): for P pairs and a session of N boards, list the candidates
  best first. A complete Mitchell when there are 5+ tables (odd number of tables: every round; even: a
  skip Mitchell, T-1 rounds), then a complete Howell, then reduced ones. The director picks one.
- **Sections and the deal** (`movement.Layouts`, `Advise`; page "Раздавания"): a large field plays in
  sections A, B…, each a Mitchell of the same rounds on **the same deals** (its own copy), scored across
  the whole field. A Mitchell section of T tables needs T sets, so a section layout needs
  copies = sections and boards 1..(largest section's tables × boards per round), at most **board 32** (the
  federation's boxes). A pair plays **exactly** the boards set (BBF: fixed, no ±). Each section may have a
  pair **sitting out** each round. An even section that needs all its rounds plays a **relay-and-stand
  Mitchell**: tables 1 and 2 share a set (the TD duplicates it), one set rests on a stand, the rest move down a
  table, EW move up without skipping. Layouts are ranked by the most comparisons per board (sections ×
  rounds), then fewer sit-outs, fewer duplicated sets, fewer sections and fewer moves; sections are 7–16
  tables. Before the day the TD gives the expected range of pairs and the boards a pair plays, and gets the
  deals worth making: 50–60 pairs, 26 boards → 2 × boards 1–30 (50: 13 + 13 tables, a pair sitting out in
  each). What was dealt is recorded, and the session's layouts keep to it (56 pairs: 2 × 14 tables, 13 × 2).
  Fewer than 50 at 26 boards needs a section of 17+ tables, i.e. tables sharing sets beyond the relay: not
  yet (a search shows such movements exist for 14–24 tables within boards 1–32).
  The session's movement is the sections combined (`movement.Combine`): tables and pairs numbered on, shown
  as "B7", boards moving within their section; `Movement.Section(i)` gives a section back for Bridgemate.
- **The BBF TD's rules** (26 boards = 13 rounds as the example):
  - a single Mitchell section is complete: **as many tables as rounds** (13); two or more sections may be
    13–16 tables (53–60 pairs: two Mitchell sections);
  - otherwise a full 13-table Mitchell and a **Howell** for the rest (39–50 pairs); several sessions prefer
    this to Mitchell sections when the Howell needs no shared sets, so later sessions reseat pairs against
    new opponents;
  - else **two Howells** (28–38 pairs), split unevenly when that avoids a sit-out;
  - with an **even number of pairs nobody sits out**; with an odd one, a sit-out in a section is fine;
  - tables sharing a set in a round take it from a **second copy on an empty table**: the plan lists those
    sets. A reduced Howell with more tables than sets (boards up to 32: 16 sets) finds its sets by local
    search, sharing as little as can be.
  - **32 pairs: Web Mitchell** (16 tables: NS stay, EW move up a table; tables 1–8 pass boards down,
    9–16 up, mirrored tables on the same set: two copies of boards 1–26); **36 pairs: Appendix Mitchell
    13+5** (a 13-table Mitchell, fixed NS at tables 1–5, travelling NS round tables 6–13 and the 5-table
    appendix, which plays the same sets as tables 1–5). Both scrambled (arrow switches in set rounds) so the
    field is one. Generated from formulas that reproduce Magic Contest's own movements row for row (its
    Bridgemate files for 16 tables and 13+5, 13 rounds).
  - The new-session page lists four different movements, the TD's preference first.
  - Pending: the 12-table Mitchell with one switch round.
  The deal advice speaks in whole boxes 1–32.
- **Relay-and-stand Mitchell** (even tables, all rounds): the first and the last table share a set, the
  stand is between tables T/2 and T/2+1, boards move down a table through it.
- **Howell** for 8+ pairs comes from a cyclic Room square, so it needs one set of boards and every table
  moves its boards on by one each round. 4 and 6 pairs have no such square; they get a round robin
  with duplicated boards (a dealing machine), as does any size on request. Odd counts get a phantom.
- **Mitchell** has two fields (NS and EW ranked apart) unless arrow-switch rounds are set to make one.
- **Scoring**: MP with Neuberg for boards played fewer times and artificial adjusted scores (60/40…);
  IMP as cross-IMP or Butler (datum with a configurable trim).
- **Sessions and streams**: the next session's seating comes from `pairs.Seat`, a search that puts pairs who
  already met apart and mixes pairs who were in the same field. Its weights are set per stage: the final
  stage can allow repeats (Meet weight 0) and seat by ranking instead.
  Note: two *complete* Mitchells over the same pairs must repeat. If the new NS line holds a pairs of the
  old one, a² + (9−a)² of 81 meetings repeat on 9 tables, so 41 is the least possible. Avoiding repeats
  needs reduced rounds or a different movement.
- **Stages**: qualifying sessions → finals A, B, … (`pairs.Qualify`), each final with its own movement and a
  carry-over: a fraction of the qualifying IMPs, or for MP a weighted percentage. The usual case is a
  2-session qualifier into a 2-session final, where the qualifying average counts 25%:
  final total = 0.25 × qualifying % + 0.75 × final %. The percentage is set per final. A tie on a cut is
  stopped for the director.
- **bridge.bg slots** (USEBIO), as the site reads them (bridge.bg#242 for the final sessions):
  - `<id>.s<k>.xml`: qualifying session k. The standings are **cumulative** after session k (the site takes
    the latest session for the overall order and for ELO, as with Magic Contest's files), with the
    session's own result beside them (`SESSION_PERCENTAGE`, `SESSION_BOARDS`), and the session's boards.
  - `<id>.f<n>.xml`: final A, B… The final's standings with the carry-over split into its parts
    (`CARRY_OVER` + `SESSION_SCORE` = the total), and the boards when it was one session.
  - `<id>.f<n>s<k>.xml`: session k of a final played in several (`tournaments.final_sessions` on the
    site, e.g. `2,1`). The final so far with the carry-over, the session's own result, and its boards.
  - One qualifying session and no finals: the tournament's own `<id>.xml`.
  - ELO comes from the finals' own files plus the latest qualifying session, never from `f<n>s<k>`.

## Teams

- **Screens (first version: Swiss, Danish, round robin)**: an event is created "по отбори"; teams are a
  name and 4-6 players, seeded in the order entered. Start fixes the format, boards per match and the
  bye's VPs (WBF 20-point scale). Each round is paired from the event page with its boards (count and
  first board). A match is entered board by board, both rooms: the open room seats the home team NS,
  the closed room the away team; an adjusted score is IMPs to each team for the board. When every board
  is in, the match's IMPs go into the event log and give the VPs; a correction records it again. The
  boards are in `team_boards` with a history, as pairs results. Standings show each round's VPs; the
  USEBIO file is the event's.
- **Groups and K/O screens**: between rounds the director splits a group by its ranking into new
  groups (name, number of teams, format, carry: all / a percentage / none / head-to-head), exchanges the
  last n of one group with the first n of another (the movers' carry chosen at the time), or changes a
  group's format from the next round. Each group can play its own number of boards in a round. A K/O
  match takes the director's carry-over in IMPs; a tie (after carry-over) is settled by extra boards,
  entered as each team's IMPs, again while still level; the next round waits for it. A group with
  nothing left to play (K/O decided, round robin complete) sits out while the others go on. Standings
  are per group, a K/O group by how far each team got; the USEBIO file ranks group after group.
- Next: BridgeMate for teams, BAM.

An event is a log of matches plus the current groups. Standings are always worked out from the log,
so a corrected score reranks everything after it.

- **Formats per group**: Swiss (top-down, no repeats, with backtracking; when a small closed group has run
  out of new opponents it allows the fewest repeats), Danish (1-2, 3-4…, repeats allowed), round robin
  (Berger), knockout (bracket by rank 1-8, 4-5, 2-7, 3-6). The TD enters the K/O carry-over by hand (`SetCarry`).
  A match level after carry-over stays *tied*, and nothing pairs past it, until extra boards
  (usually 1 or 2 at a time, `AddPlayoff`) decide it.
- **Changing groups between rounds**: `Split` (the field, or a group, cut by standings into A = top N, B, C…
  — this is also "closing" a group) and `Exchange` (bottom n of A ↔ top n of B). A group's format can be
  changed between rounds (Swiss → Danish). At an exchange the director decides what the moving teams
  bring: their score, a percentage of it, nothing, or head-to-head. The teams that stay keep theirs.
- **Carry-over per group**: all VPs, none, or head-to-head (VPs against the teams of the new group), times
  a fraction.
- **Byes**: the lowest-ranked team that has had the fewest byes, with a set number of VPs (none in a knockout).
- **Tie-breaks**: VP, then head-to-head VP among the tied teams, IMP quotient, seed.
- **VP**: WBF continuous 20-point scale (the formula, rounded to 2 decimals); the scale is an interface,
  so a federation table can replace it. Bye VPs are set per event.
- **BAM**: per board 1 / ½ / 0 by the two tables' net score.

The examples this was built against (tests in `internal/teams`):
1. 31 teams: 7 rounds Swiss → 2 rounds Danish → A = top 8 knockout, B = the rest, Swiss with carry-over.
2. 24 teams: 8 rounds Swiss → close A with 10 (Swiss inside A, rest B) → after round 10 the last two of A
   and the first two of B swap.

bridge.bg reads a team event's groups from `ScoreCarryOver` offsets (1000, 2000…) and its stages from
`<id>.s<k>.pbn`; the PBN writer must follow `../bridge.bg/next/src/Results/PbnParser.php`.

## Results files

USEBIO is the main format: `<id>.s<k>.xml`, `<id>.f<n>.xml`, `<id>.xml` go into the same slots as
`.pbn`. The tests run `../bridge.bg/next/src/Results/UsebioParser.php` on what the writer makes,
so a change on either side that breaks the other fails here.

- MP traveller lines carry each side's **percentage** in `NS_MATCH_POINTS` / `EW_MATCH_POINTS`,
  because bridge.bg shows those values as the board's percentage. Strict USEBIO puts matchpoints there.
- **Team groups**: each team carries a `<GROUP>` (A, B, C; not in USEBIO itself), and matches carry
  `TEAM_IMPS` / `OPPOSING_TEAM_IMPS`. bridge.bg reads both from PR #240 on (zdraganov/bridge.bg), turning
  the groups into the same VP markers as a Magic Contest .pbn. Set `BRIDGEBG_DIR` to run the tests
  against another checkout of the site (a worktree).
- *To check against the USEBIO 1.2 spec: `TEAM_IMPS` / `OPPOSING_TEAM_IMPS` are this writer's names for
  the match IMPs.*

## BridgeMate

BridgeMate Control Software (BCS) talks to the scoring program through an Access database (`.bws`).
Score52 writes the event's sections, tables, round data (table, round, NS, EW, low/high board) and
player numbers into it, starts BCS on it, and polls `ReceivedData` for results, which go into the
event log as they arrive. A movement change mid-session rewrites `RoundData` for the remaining rounds.

Access needs Windows (the Access ODBC driver comes with BCS). On macOS during development the `.bws`
side is faked by a file-backed implementation of the same interface; tests run against that. The
Windows VM in `../bridge.bg/tools/magic-vm` can host BCS for integration testing.

The format, the startup switches, team games, movement updates and the traps in Bridgemate's own
templates are in [bridgemate.md](bridgemate.md), from the developer's guide rev. 54 (BCS 4.3.8).

## bridge.bg

The same `td_` token flow as results-sync (`/admin/td-connect`, `POST /api/td_connect`, `X-Td-Token`):

- **out**: `POST /api/td_results` with the file named after its slot. No change on the site.
- **in**: the tournament list (`GET /api/td_tournaments.php`) exists. Registered pairs/teams and their
  players (with federation ids, for ELO) need a new read-only endpoint, e.g.
  `GET /api/td_entries?tournament=<id>`, scoped to the token's clubs like the uploads.

## Decided with the federation (2026-11-27)

1. **Streams**: mix the fields and avoid repeat opponents across sessions; the final stage may repeat.
2. **Exchange carry-over**: the director's choice per exchange: keep the score or a percentage of it.
3. **VP scale**: the WBF formula.
4. **MP carry-over**: weighted percentage, typically 25% for the qualifying average.
5. **Knockout**: the TD enters carry-over by hand; ties are played off with 1–2 extra boards.

Still open: the team tie-break order in the federation's regulations (VP, head-to-head, IMP quotient,
seed for now).
