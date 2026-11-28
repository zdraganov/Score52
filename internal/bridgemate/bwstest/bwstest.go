// Package bwstest makes a stand-in for a BridgeMate .bws in SQLite: the template's tables with the
// same names, which take the same [bracketed] SQL, so the code that writes and reads .bws files runs
// unchanged in tests and on machines without the Access driver.
package bwstest

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// Schema is the template's tables, as far as Score52 uses them. PlayerNumbers lacks EntrySource and
// Round, as the template does.
var Schema = []string{
	"CREATE TABLE [Clients] ([ID] INTEGER PRIMARY KEY AUTOINCREMENT, [Computer] TEXT)",
	"CREATE TABLE [Session] ([ID] INTEGER, [Name] TEXT, [Date] DATETIME, [Time] DATETIME, [GUID] TEXT, [Status] INTEGER, [ShowInApp] BOOLEAN, [PairsMoveAcrossField] BOOLEAN, [EWReturnHome] BOOLEAN)",
	"CREATE TABLE [Section] ([ID] INTEGER, [Letter] TEXT, [Tables] INTEGER, [MissingPair] INTEGER, [EWMoveBeforePlay] INTEGER, [Session] INTEGER, [ScoringType] INTEGER, [Winners] INTEGER)",
	"CREATE TABLE [Tables] ([Section] INTEGER, [Table] INTEGER, [ComputerID] INTEGER, [Status] INTEGER, [LogOnOff] INTEGER, [CurrentRound] INTEGER, [CurrentBoard] INTEGER, [UpdateFromRound] INTEGER, [Group] INTEGER)",
	"CREATE TABLE [RoundData] ([Section] INTEGER, [Table] INTEGER, [Round] INTEGER, [NSPair] INTEGER, [EWPair] INTEGER, [LowBoard] INTEGER, [HighBoard] INTEGER, [CustomBoards] TEXT CHECK ([CustomBoards] IS NULL OR [CustomBoards] <> ''))",
	"CREATE TABLE [PlayerNumbers] ([Section] INTEGER, [Table] INTEGER, [Direction] TEXT, [Number] TEXT, [Name] TEXT, [Updated] BOOLEAN, [TimeLog] DATETIME, [Processed] BOOLEAN)",
	"CREATE TABLE [ReceivedData] ([ID] INTEGER PRIMARY KEY AUTOINCREMENT, [Section] INTEGER, [Table] INTEGER, [Round] INTEGER, [Board] INTEGER, [PairNS] INTEGER, [PairEW] INTEGER, [Declarer] INTEGER, [NS/EW] TEXT, [Contract] TEXT, [Result] TEXT, [LeadCard] TEXT, [Remarks] TEXT, [DateLog] DATETIME, [TimeLog] DATETIME, [Processed] BOOLEAN, [Erased] BOOLEAN, [custom_ProcessedMC] BOOLEAN)",
}

// Create makes an empty stand-in at path.
func Create(path string) error {
	db, err := Open(path)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, q := range Schema {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// Open opens a stand-in.
func Open(path string) (*sql.DB, error) { return sql.Open("sqlite", path) }
