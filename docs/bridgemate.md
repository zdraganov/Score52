# BridgeMate integration notes

Source: Bridgemate developer's guide, revision 54 (BCS 4.3.8),
<https://www.bridgemate.com/resources/developer/BMdevguide.pdf>, and the templates at
<https://www.bridgemate.com/resources/developer/> (`Template_Access2000.bws`, `Template_Access97.bws`,
`samplefiles.zip` = the same two templates). These notes are what Score52 needs from it, mapped
onto our engine. Read the guide itself for anything not here.

## The file

- A `.bws` is an MS Access database (Access 97 or 2000 format, no password). BCS (Bridgemate Control
  Software) opens it, talks to the base station, and writes results back into it. Score52 never
  talks to the hardware.
- Read and write it over ODBC (Windows Access driver, installed with BCS) without locking it, since BCS
  has it open at the same time.
- **Start from `Template_Access2000.bws`**, not the Access 97 one:
  - the Access 97 template misspells `Session.PairsMoveAcrrossField`;
  - both templates predate the current guide: `PlayerNumbers` has no `EntrySource` and `Settings` has no
    `BM3*` columns. After copying the template, add the missing columns (`ALTER TABLE`) before use;
  - column order differs between the templates (`ReceivedData`): always address columns by name.
- Columns we add ourselves must be named `custom_...` and go at the end of a table.
- No indexes needed; every field is non-required.

## What Score52 writes before a session

| Table | Rows | Our source |
|---|---|---|
| `Clients` | one: `Computer` = this PC's name exactly as Windows reports it (case-sensitive); read back its `ID` | the machine |
| `Session` | one per session: `ID`, `Name` (≤40 chars), `Date`, `Time`, `GUID` (keep it: rewriting the row must reuse it), `Status` 0 | `sessions` |
| `Section` | one per section: `ID` 1..78, `Letter` (A..Z, AA..ZZ, AAA..ZZZ, same letter repeated), `Tables`, `MissingPair`, `EWMoveBeforePlay` 0, `Session`, `ScoringType`, `Winners` | movement |
| `Tables` | one per table: `Section`, `Table`, `ComputerID` = the client's ID, `Group` | movement |
| `RoundData` | one per table × round: `NSPair`, `EWPair`, `LowBoard`, `HighBoard` (or `CustomBoards` "1,2,5" ascending, with Low/High still > 0) | `movement.Seats` |
| `PlayerNumbers` | optional: 4 per table (N, S, E, W) with `Name` (≤18 chars), `Updated` = true, `EntrySource` = 0 | entries' players |
| `PlayerNames` | optional: the federation's players (`ID` long or `strID` text, `Name`), so BCS fills names itself from numbers | bridge.bg players |
| `Settings` | optional: one row for all sections, or one per section; only the columns we want to control | event settings |
| `HandRecord` | optional: per section and board, 16 suit fields, cards `AKQJT98...` (T for ten) | dealing machine files |

`ReceivedData`, `IntermediateData`, `BiddingData`, `PlayData` stay empty; BCS fills them.

### Mapping

- `ScoringType`: 1 MP, 2 Butler, 3 cross-IMP, 4 teams.
- `Winners`: 2 for a two-field Mitchell (`movement.TwoFields`; NS must then stay NS all session, EW stay
  EW, which a Mitchell without arrow switches guarantees), 1 for Howell / one field. Never 0.
- Pair numbers: our Mitchell numbers EW pairs T+1..2T, so NS and EW numbers differ. A sit-out then
  goes in `Section.MissingPair` = the phantom's number. (With equal NS/EW numbering, write 0 for the
  missing side in `RoundData` instead.) Empty table in a round: all four of NS/EW/Low/High = 0.
- Every table must have a row for every round, including rounds it doesn't play.
- Limits (Bridgemate II/III): 256 tables per server, table numbers ≤4095, pair numbers ≤4095,
  board numbers ≤127, ≤54 boards per round, ≤255 rounds, and per table rounds + boards ≤ 256.
  Score52 must check these before writing.

## Starting BCS

`BMPro.exe /f:[C:\path\event.bws] /s /r` (path in square brackets):

| Switch | Meaning |
|---|---|
| `/f:[file]` | open this database |
| `/s` | with /f: reset the server and upload the tables |
| `/r` | with /f: start reading results, no report after the upload |
| `/m` | start minimised |
| `/h:[1,2]` | with /s: also upload hand records of these section IDs |
| `/t1`…`/t5` | open on tab Results / Round monitor / Board monitor / Result matrix / Player names |
| `/pi` | Bridgemate III + II together: a second instance for the II server |

Restarting BCS on the same file without `/s` resumes a session (if the director closed BCS by mistake).

## Reading results

Poll `ReceivedData WHERE Processed = false` every few seconds, process the rows, set `Processed` = true.
Each entry or re-entry is a **new row** (autonumber `ID`); the latest row for a table/round/board wins.

| Field | Content |
|---|---|
| `Section`, `Table`, `Round`, `Board`, `PairNS`, `PairEW` | where, as uploaded |
| `Declarer` | the declaring pair's number |
| `NS/EW` | the declarer's direction: `N`, `E`, `S`, `W` (or `NS` / `EW`) |
| `Contract` | `"4 S"`, `"3 NT x"`, `"2 H xx"`, `"PASS"`, with spaces between parts; empty with an arbitral score or no play |
| `Result` | `"="`, `"+1"`…`"+6"`, `"-1"`…`"-13"` relative to the contract; empty with PASS, arbitral or no play |
| `LeadCard` | `"S2"`, `"DA"`, `"C10"` (ten as **10** here, unlike the hand records' T) |
| `Remarks` | `"Not played"`, `"Arbitral score"` (TD to decide), `"60%-40%"` … (NS%-EW% of 40/50/60), `"Wrong direction"` |
| `Erased` | true: the score was deleted on a Bridgemate; drop it |
| `SuspiciousContract` | 0 / 1 flagged / 2 flagged and confirmed by the TD |
| `DateLog`, `TimeLog` | when it arrived |

`PlayerNumbers`: BCS writes the numbers players enter (per direction; `Round` 0 = before round 1, or the
round, when numbers are entered every round as in teams). Rows BCS changes get `Processed` = false. We
look the number up and write `Name` + `Updated` = true + `EntrySource` = 0.

## Correcting a result from Score52

Update the `ReceivedData` row and set `ExternalUpdate` = **-1** in the same statement. BCS picks it up
within ~15 s and the Bridgemates show the new score. BCS does not validate it, so Score52 must
(e.g. no 7NT+1).

## Changing the movement mid-session

Edit `RoundData` first, then set `Tables.UpdateFromRound` on the affected tables:

| Value | Effect |
|---|---|
| `n` | re-upload from round n. Results of round n and later are **erased from the server** |
| `n + 256` | add round n and keep the Bridgemates waiting for more rounds afterwards (Swiss) |
| `-n` | remove rounds n and later, upload nothing |
| `999` | remove the table (delete its `RoundData` first) |
| `998` | reset that Bridgemate (II) |
| `997` | push changed `Settings` (II) |
| `996` | re-read the section's hand records (any one table of the section) |

New tables: add `RoundData`, then the `Tables` row, then `UpdateFromRound` = 1. BCS resets the field to
0 when done.

Swiss rounds: start the session with round 1 and set `UpdateFromRound` = 257 so Bridgemates wait after
round 1. Each next round: write it and set `n + 256`. The last round: set plain `n`, which ends the session.

## Team games

As Magic Contest lays them out (its teams `.bws`, dumped 2026-11-28), and as Score52 writes them:

- **One `.bws` per round** (`<event>-r<round>.bws` next to the event file). Bridgemate cannot regroup tables
  within a file, so the next round is a new file and BCS is restarted on it (close the old BCS first:
  it runs as a single instance and a second start does nothing).
- **Section A (ID 1) is the open room, section B (ID 2) the closed room**, one table per match in each;
  match k is table k in both, `Tables.Group` = k. `ScoringType` = 4, `Winners` = 1, `RoundData.Round` = 1.
- Pair numbers: Magic Contest uses placeholders (A: NS t, EW 200+t; B: NS 100+t, EW 300+t). Score52 writes
  the teams' own numbers (their seeds): at A the home team NS and the away team EW, at B the other way
  round, so the Bridgemates show the team numbers. Results are matched by section and table, not by
  pair number.
- No player names are preloaded: a team's line-up changes from round to round. Magic Contest sets
  `Settings.BM2NumberEntryEachRound` so players enter their numbers every round.
- Reading: section 1 → open room, 2 → closed room, table → match. A board the director entered or
  corrected in Score52 is not overwritten; a result erased on a Bridgemate is removed. Adjusted,
  "not played" and "arbitral" results are counted for the director, who enters them as IMPs.

## Checked in the VM (2026-11-27)

In `../bridge.bg/tools/magic-vm` (Windows 11 ARM, BCS 4.3.8 at
`C:\Program Files (x86)\Bridgemate Pro\BMPro.exe`, Magic Contest):

- **Only the 32-bit Jet driver exists** (`Microsoft Access Driver (*.mdb)` under `WOW6432Node`), with no
  64-bit Access driver. Score52's Windows build is therefore `GOARCH=386`. That runs on 64-bit Windows,
  and on ARM through emulation. `github.com/alexbrainman/odbc` builds for it with `CGO_ENABLED=0`.
- Through that driver (tested with the 32-bit script host, as the Go probe was blocked; see below):
  copying `Template_Access2000.bws` and `ALTER TABLE ... ADD COLUMN` for the missing columns works,
  inserts into every setup table work, and **Cyrillic names round-trip** (Unicode).
- **`RoundData.CustomBoards` rejects `''`** in the template ("cannot be a zero-length string"), although
  the guide says it may be empty. Write NULL or leave the column out.
- A `.bws` that BCS and Magic Contest have open can be read at the same time.
- Magic Contest's `.bws` (`Documents\Magic Contest Inbox`) has extra columns of its own and of newer BCS
  (`Section.OnlineEventGUID`, `OnlineEventRoundDuration`; `Clients.ShutDownBPCNow`, `UseBWSSettings`;
  `PlayerNumbers.UpdatedHlp`; many `custom_*`). Tolerate unknown columns; address known ones by name.
- In that file: `Contract` `"6 NT"`, `"4 H x"`, `"1 C xx"`; `Result` `"="`, `"+2"`, `"-1"`; `Declarer` is the
  declaring pair's number and `NS/EW` the declarer's seat (`N`/`E`/`S`/`W`). Those rows came from the VM's
  `bws-round.vbs`, not from Bridgemate hardware, so they show what Magic Contest accepts. Hardware
  output still needs confirming from a real event's file.
- **Smart App Control blocks unsigned programs** on a fresh Windows 11 (`VerifiedAndReputablePolicyState`
  = 1): our unsigned `.exe` was refused ("blocked by your organization's Device Guard policy"). It would
  be refused the same way on a director's PC with it on. That is a reason to code-sign the `.exe` (and
  results-sync).

- **Score52's own code, end to end on Windows** (`cmd/bwstool`, `GOARCH=386`): `create` copied the
  template and wrote a 5-table Mitchell with Cyrillic names through `bridgemate.Write`, with client
  `MAGIC-VM` from `GetComputerNameW`. `bws-round.vbs` added two rounds of results to it as BCS would
  (it needs Magic Contest's `ReceivedData.custom_ProcessedMC` column, added for the test). `read`
  parsed all 30 rows through `ReadReceived` / `Current` and scored them. Smart App Control had to be
  turned off in the VM for the unsigned `.exe` to run.

- **Teams, end to end (2026-11-28)**: `score52.exe` (windows/386) in the VM made a round's `.bws` from the
  page, started BCS on it (`"BMPro.exe" /f:[path] /s /r`, written out as the raw command line: BCS
  takes everything after `/f:[` up to the last character as the path, so a quoted argument leaves a `]`
  in it), and BCS showed sections A and B with 3 tables each. `bws-round.vbs` (without Magic Contest's
  `custom_ProcessedMC` column) added 24 results while BCS had the file open; the page read them through
  the Access driver and recorded the three matches with IMPs and VPs.

## Still open

1. Bundle Bridgemate's template or build our own empty Access file: check the template's licence.
2. Open a Score52-made file in BCS (`/f:[...]`, then `/s /r`). BCS seems to run as a single
   instance: a second launch while it is open on another file did nothing.
3. Real hardware `ReceivedData` rows (adjusted scores, wrong direction, erased) from an event's file.
