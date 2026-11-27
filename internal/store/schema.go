package store

// migrations, in order; migration i takes the file to user_version i+1. Never edit one that has shipped:
// add another.
var migrations = []string{
	`
CREATE TABLE meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE players (
	id            INTEGER PRIMARY KEY,
	name          TEXT NOT NULL,
	federation_id TEXT NOT NULL DEFAULT '',   -- players.ibn on bridge.bg
	club          TEXT NOT NULL DEFAULT ''
);

-- a pair or a team
CREATE TABLE entries (
	id   INTEGER PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	seed INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE entry_players (
	entry_id  INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
	seat      INTEGER NOT NULL,
	player_id INTEGER NOT NULL REFERENCES players(id),
	PRIMARY KEY (entry_id, seat)
);

-- a pairs session or final: its movement and who sits at which movement number
CREATE TABLE sessions (
	id               INTEGER PRIMARY KEY,
	slot             TEXT NOT NULL UNIQUE,      -- bridge.bg's file slot: s1, s2, f1...
	name             TEXT NOT NULL DEFAULT '',
	scoring          TEXT NOT NULL,             -- mp, cross-imp, butler
	boards_per_round INTEGER NOT NULL,
	first_board      INTEGER NOT NULL DEFAULT 1,
	movement         TEXT NOT NULL,             -- movement.Movement as JSON
	seating          TEXT NOT NULL,             -- JSON [entry id at movement number 1, 2...], 0 = phantom
	status           TEXT NOT NULL DEFAULT 'planned'
);

-- traveller lines, by movement pair numbers as BridgeMate reports them
CREATE TABLE results (
	session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	board      INTEGER NOT NULL,
	round      INTEGER NOT NULL,
	tbl        INTEGER NOT NULL,
	ns         INTEGER NOT NULL,
	ew         INTEGER NOT NULL,
	contract   TEXT NOT NULL DEFAULT '',        -- 4S, 3NTX, PASS; '' with an adjusted score
	declarer   TEXT NOT NULL DEFAULT '',
	lead       TEXT NOT NULL DEFAULT '',
	tricks     INTEGER NOT NULL DEFAULT 0,
	score      INTEGER NOT NULL DEFAULT 0,      -- NS
	adj_ns     REAL,                            -- artificial adjusted score, % (or IMPs)
	adj_ew     REAL,
	source     TEXT NOT NULL DEFAULT 'manual',  -- manual, bridgemate
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	PRIMARY KEY (session_id, board, ns, ew)
);

-- every change to a result, for the director: what it was before
CREATE TABLE result_history (
	id         INTEGER PRIMARY KEY,
	session_id INTEGER NOT NULL,
	board      INTEGER NOT NULL,
	ns         INTEGER NOT NULL,
	ew         INTEGER NOT NULL,
	action     TEXT NOT NULL,                   -- update, delete
	old        TEXT NOT NULL,                   -- the row as JSON
	at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TRIGGER results_update AFTER UPDATE ON results BEGIN
	INSERT INTO result_history (session_id, board, ns, ew, action, old)
	VALUES (OLD.session_id, OLD.board, OLD.ns, OLD.ew, 'update', json_object(
		'contract', OLD.contract, 'declarer', OLD.declarer, 'lead', OLD.lead, 'tricks', OLD.tricks,
		'score', OLD.score, 'adj_ns', OLD.adj_ns, 'adj_ew', OLD.adj_ew, 'source', OLD.source, 'updated_at', OLD.updated_at));
END;

CREATE TRIGGER results_delete AFTER DELETE ON results BEGIN
	INSERT INTO result_history (session_id, board, ns, ew, action, old)
	VALUES (OLD.session_id, OLD.board, OLD.ns, OLD.ew, 'delete', json_object(
		'contract', OLD.contract, 'declarer', OLD.declarer, 'lead', OLD.lead, 'tricks', OLD.tricks,
		'score', OLD.score, 'adj_ns', OLD.adj_ns, 'adj_ew', OLD.adj_ew, 'source', OLD.source, 'updated_at', OLD.updated_at));
END;

-- a teams event is its log of operations (teams.Op), replayed on load
CREATE TABLE team_ops (
	seq INTEGER PRIMARY KEY,
	op  TEXT NOT NULL,
	at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
`,
}
