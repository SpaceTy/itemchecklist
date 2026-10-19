package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	previous := db
	testDB, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "test.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	testDB.SetMaxOpenConns(1)
	if _, err := testDB.Exec(schemaSQL); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db = testDB
	t.Cleanup(func() {
		testDB.Close()
		db = previous
	})
}

func insertChangeNode(t *testing.T, id int64, listID string, parent any, kind, createdAt string, prunable bool) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO change_nodes(id, list_id, parent_id, actor, kind, summary, snapshot, created_at, prunable)
		 VALUES(?, ?, ?, 'tester', ?, ?, '{}', ?, ?)`,
		id,
		listID,
		parent,
		kind,
		kind,
		createdAt,
		boolToInt(prunable),
	)
	if err != nil {
		t.Fatalf("insert change node %d: %v", id, err)
	}
}

func TestPruneChangesReparentsAndKeepsHead(t *testing.T) {
	setupTestDB(t)

	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	recent := time.Now().UTC().Format(time.RFC3339)

	if _, err := db.Exec(
		`INSERT INTO lists(id, name, owner_username, created_at, updated_at, head_change_id)
		 VALUES('list_test', 'Test', 'tester', ?, ?, 5)`,
		recent, recent,
	); err != nil {
		t.Fatalf("insert list: %v", err)
	}

	insertChangeNode(t, 1, "list_test", nil, "list_created", old, false)
	insertChangeNode(t, 2, "list_test", 1, "gathered_updated", old, true)
	insertChangeNode(t, 3, "list_test", 2, "gathered_updated", recent, true)
	insertChangeNode(t, 4, "list_test", 3, "claim_updated", old, false)
	insertChangeNode(t, 5, "list_test", 4, "gathered_updated", old, true)
	insertChangeNode(t, 6, "list_test", 2, "gathered_updated", recent, true)

	if err := pruneChanges(time.Now().UTC()); err != nil {
		t.Fatalf("prune: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM change_nodes WHERE id = 2`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected old amount change #2 to be pruned")
	}

	for _, id := range []int64{1, 3, 4, 5, 6} {
		if err := db.QueryRow(`SELECT COUNT(*) FROM change_nodes WHERE id = ?`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected change #%d to be retained", id)
		}
	}

	var parent sql.NullInt64
	if err := db.QueryRow(`SELECT parent_id FROM change_nodes WHERE id = 6`).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if !parent.Valid || parent.Int64 != 1 {
		t.Fatalf("expected node #6 to be reparented to #1, got %+v", parent)
	}
}

func TestRollbackCreatesBranchNode(t *testing.T) {
	setupTestDB(t)

	now := nowRFC3339()
	list := itemList{
		ID:            "list_test",
		Name:          "Test",
		OwnerUsername: "tester",
		Collaborators: []string{},
		InviteCodes:   []inviteCode{},
		Items: []item{{
			Name:          "Stone",
			Target:        10,
			Gathered:      0,
			Claims:        []claim{},
			Contributions: []contribution{},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := writeLists([]itemList{list}, changeMeta{Actor: "tester", Kind: "list_created", Summary: "Created"}); err != nil {
		t.Fatalf("create list: %v", err)
	}

	updated := list
	updated.UpdatedAt = nowRFC3339()
	updated.Items[0].Gathered = 5
	updated.Items[0].Contributions = []contribution{{Username: "tester", Amount: 5}}
	if err := writeLists([]itemList{updated}, changeMeta{Actor: "tester", Kind: "gathered_updated", Summary: "Updated amount"}); err != nil {
		t.Fatalf("update list: %v", err)
	}

	restored, err := rollbackList(list.ID, 1, "tester")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if len(restored.Items) != 1 || restored.Items[0].Gathered != 0 {
		t.Fatalf("unexpected restored state: %+v", restored.Items)
	}

	var (
		parent sql.NullInt64
		kind   string
		head   int64
	)
	if err := db.QueryRow(`SELECT parent_id, kind FROM change_nodes WHERE id = 3`).Scan(&parent, &kind); err != nil {
		t.Fatalf("read rollback node: %v", err)
	}
	if !parent.Valid || parent.Int64 != 1 || kind != "rollback" {
		t.Fatalf("unexpected rollback node parent=%+v kind=%q", parent, kind)
	}
	if err := db.QueryRow(`SELECT head_change_id FROM lists WHERE id = ?`, list.ID).Scan(&head); err != nil {
		t.Fatal(err)
	}
	if head != 3 {
		t.Fatalf("expected head change 3, got %d", head)
	}
}
