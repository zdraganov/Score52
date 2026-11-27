# Scorer — design

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
cmd/scorer/          main: flags, mode, opens the browser
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
- **bridge.bg slots**: session k → `<id>.s<k>.pbn`, final A/B → `<id>.f1/.f2.pbn`, the event → `<id>.pbn`.

## Teams

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
The scorer writes the event's sections, tables, round data (table, round, NS, EW, low/high board) and
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
