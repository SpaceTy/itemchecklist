package main

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

const schemaSQL = `
CREATE TABLE IF NOT EXISTS users (
	username      TEXT PRIMARY KEY,
	password_hash TEXT NOT NULL,
	admin         INTEGER NOT NULL DEFAULT 0,
	frozen        INTEGER NOT NULL DEFAULT 0,
	deleted       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS settings (
	id                       INTEGER PRIMARY KEY CHECK (id = 1),
	registration_locked_down INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS lists (
	id             TEXT PRIMARY KEY,
	name           TEXT NOT NULL,
	owner_username TEXT NOT NULL,
	created_at     TEXT NOT NULL,
	updated_at     TEXT NOT NULL,
	deleted        INTEGER NOT NULL DEFAULT 0,
	head_change_id INTEGER
);

CREATE TABLE IF NOT EXISTS list_collaborators (
	list_id  TEXT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
	username TEXT NOT NULL,
	PRIMARY KEY (list_id, username)
);

CREATE TABLE IF NOT EXISTS list_invites (
	list_id    TEXT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
	code       TEXT NOT NULL,
	created_at TEXT NOT NULL,
	created_by TEXT NOT NULL,
	PRIMARY KEY (list_id, code)
);

CREATE TABLE IF NOT EXISTS items (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	list_id   TEXT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
	name      TEXT NOT NULL,
	target    INTEGER NOT NULL DEFAULT 0,
	gathered  INTEGER NOT NULL DEFAULT 0,
	position  INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS items_list_name ON items(list_id, name);

CREATE TABLE IF NOT EXISTS item_contributions (
	item_id  INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	username TEXT NOT NULL,
	amount   INTEGER NOT NULL,
	PRIMARY KEY (item_id, username)
);

CREATE TABLE IF NOT EXISTS item_claims (
	item_id     INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	claimer     TEXT NOT NULL,
	claim_start INTEGER NOT NULL,
	claim_end   INTEGER NOT NULL,
	PRIMARY KEY (item_id, claimer)
);

CREATE TABLE IF NOT EXISTS change_nodes (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	list_id    TEXT NOT NULL,
	parent_id  INTEGER,
	actor      TEXT NOT NULL,
	kind       TEXT NOT NULL,
	summary    TEXT NOT NULL,
	snapshot   TEXT NOT NULL,
	created_at TEXT NOT NULL,
	prunable   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS change_nodes_list ON change_nodes(list_id, id);
CREATE INDEX IF NOT EXISTS change_nodes_parent ON change_nodes(parent_id);
`

func openDatabase() error {
	var err error
	db, err = sql.Open("sqlite3", dbPath+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		return err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return err
	}
	return importLegacyData()
}

func importLegacyData() error {
	if err := importLegacyUsers(); err != nil {
		return err
	}
	if err := importLegacySettings(); err != nil {
		return err
	}
	return importLegacyLists()
}

func importLegacyUsers() error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if _, err := os.Stat(usersPath); os.IsNotExist(err) {
		return nil
	}

	var users []user
	if err := readJSONFile(usersPath, &users); err != nil {
		return err
	}
	if len(users) == 0 {
		return nil
	}
	if err := writeUsers(users); err != nil {
		return err
	}
	log.Printf("Imported %d users from %s into SQLite", len(users), usersPath)
	return nil
}

func importLegacySettings() error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		return nil
	}

	var settings appSettings
	if err := readJSONFile(settingsPath, &settings); err != nil {
		return err
	}
	if err := writeSettings(settings); err != nil {
		return err
	}
	log.Printf("Imported settings from %s into SQLite", settingsPath)
	return nil
}

func importLegacyLists() error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM lists`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	if _, err := os.Stat(listsPath); err == nil {
		var lists []itemList
		if err := readJSONFile(listsPath, &lists); err != nil {
			return err
		}
		if len(lists) > 0 {
			if err := writeLists(lists, changeMeta{
				Actor:   "system",
				Kind:    "lists_imported",
				Summary: "Imported legacy lists into SQLite",
			}); err != nil {
				return err
			}
			log.Printf("Imported %d lists from %s into SQLite", len(lists), listsPath)
			return nil
		}
	}

	legacyItems, err := readLegacyItems()
	if err != nil {
		return err
	}
	if len(legacyItems) == 0 {
		return nil
	}

	owner, err := firstActiveUsername()
	if err != nil {
		return err
	}
	if owner == "" {
		log.Printf("Found legacy %s but no users exist; skipping import", itemsPath)
		return nil
	}

	now := nowRFC3339()
	list := itemList{
		ID:            newID("list"),
		Name:          "Imported Checklist",
		OwnerUsername: owner,
		Collaborators: []string{},
		InviteCodes:   []inviteCode{},
		Items:         legacyItems,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := writeLists([]itemList{list}, changeMeta{
		Actor:   "system",
		Kind:    "list_created",
		Summary: "Imported legacy items into SQLite",
	}); err != nil {
		return err
	}
	log.Printf("Imported legacy %s into SQLite", itemsPath)
	return nil
}

// queryer is implemented by both *sql.DB and *sql.Tx, letting store helpers run
// either standalone or inside a transaction.
type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func firstActiveUsername() (string, error) {
	var username string
	err := db.QueryRow(`SELECT username FROM users WHERE deleted = 0 ORDER BY admin DESC, username LIMIT 1`).Scan(&username)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return username, err
}
