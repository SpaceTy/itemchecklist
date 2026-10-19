package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

type changeMeta struct {
	Actor   string
	Kind    string
	Summary string
}

// readLists returns every visible (not deleted) list.
func readLists() ([]itemList, error) {
	return readListsQ(db, false)
}

// readListsIncludingDeleted includes soft-deleted lists. Only admins should
// expose these through the API.
func readListsIncludingDeleted() ([]itemList, error) {
	return readListsQ(db, true)
}

func readListsWithIndex(listID string) ([]itemList, int, error) {
	lists, err := readListsIncludingDeleted()
	if err != nil {
		return nil, -1, err
	}
	for i := range lists {
		if lists[i].ID == listID {
			return lists, i, nil
		}
	}
	return lists, -1, nil
}

func readListsQ(q queryer, includeDeleted bool) ([]itemList, error) {
	query := `SELECT id, name, owner_username, created_at, updated_at, deleted, COALESCE(head_change_id, 0)
	          FROM lists`
	if !includeDeleted {
		query += ` WHERE deleted = 0`
	}
	query += ` ORDER BY created_at, id`

	rows, err := q.Query(query)
	if err != nil {
		return nil, err
	}

	lists := []itemList{}
	for rows.Next() {
		var (
			list    itemList
			deleted int
		)
		if err := rows.Scan(
			&list.ID,
			&list.Name,
			&list.OwnerUsername,
			&list.CreatedAt,
			&list.UpdatedAt,
			&deleted,
			&list.HeadChangeID,
		); err != nil {
			rows.Close()
			return nil, err
		}
		list.Deleted = deleted != 0
		lists = append(lists, list)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for i := range lists {
		if err := loadListRelations(q, &lists[i]); err != nil {
			return nil, err
		}
	}
	return lists, nil
}

func loadListRelations(q queryer, list *itemList) error {
	list.Collaborators = []string{}
	list.InviteCodes = []inviteCode{}
	list.Items = []item{}

	collaboratorRows, err := q.Query(
		`SELECT username FROM list_collaborators WHERE list_id = ? ORDER BY username`,
		list.ID,
	)
	if err != nil {
		return err
	}
	for collaboratorRows.Next() {
		var username string
		if err := collaboratorRows.Scan(&username); err != nil {
			collaboratorRows.Close()
			return err
		}
		list.Collaborators = append(list.Collaborators, username)
	}
	if err := collaboratorRows.Err(); err != nil {
		collaboratorRows.Close()
		return err
	}
	collaboratorRows.Close()

	inviteRows, err := q.Query(
		`SELECT code, created_at, created_by FROM list_invites WHERE list_id = ? ORDER BY created_at, code`,
		list.ID,
	)
	if err != nil {
		return err
	}
	for inviteRows.Next() {
		var invite inviteCode
		if err := inviteRows.Scan(&invite.Code, &invite.CreatedAt, &invite.CreatedBy); err != nil {
			inviteRows.Close()
			return err
		}
		list.InviteCodes = append(list.InviteCodes, invite)
	}
	if err := inviteRows.Err(); err != nil {
		inviteRows.Close()
		return err
	}
	inviteRows.Close()

	itemRows, err := q.Query(
		`SELECT id, name, target, gathered FROM items WHERE list_id = ? ORDER BY position, id`,
		list.ID,
	)
	if err != nil {
		return err
	}

	itemIDs := []int64{}
	for itemRows.Next() {
		var (
			itemID int64
			it     item
		)
		if err := itemRows.Scan(&itemID, &it.Name, &it.Target, &it.Gathered); err != nil {
			itemRows.Close()
			return err
		}
		it.Claims = []claim{}
		it.Contributions = []contribution{}
		list.Items = append(list.Items, it)
		itemIDs = append(itemIDs, itemID)
	}
	if err := itemRows.Err(); err != nil {
		itemRows.Close()
		return err
	}
	itemRows.Close()

	if len(itemIDs) == 0 {
		return nil
	}

	contributionRows, err := q.Query(
		`SELECT c.item_id, c.username, c.amount
		 FROM item_contributions c
		 JOIN items i ON i.id = c.item_id
		 WHERE i.list_id = ?
		 ORDER BY c.item_id, c.username`,
		list.ID,
	)
	if err != nil {
		return err
	}
	indexByItemID := make(map[int64]int, len(itemIDs))
	for i, itemID := range itemIDs {
		indexByItemID[itemID] = i
	}
	for contributionRows.Next() {
		var (
			itemID int64
			c      contribution
		)
		if err := contributionRows.Scan(&itemID, &c.Username, &c.Amount); err != nil {
			contributionRows.Close()
			return err
		}
		if idx, ok := indexByItemID[itemID]; ok {
			list.Items[idx].Contributions = append(list.Items[idx].Contributions, c)
		}
	}
	if err := contributionRows.Err(); err != nil {
		contributionRows.Close()
		return err
	}
	contributionRows.Close()

	claimRows, err := q.Query(
		`SELECT c.item_id, c.claimer, c.claim_start, c.claim_end
		 FROM item_claims c
		 JOIN items i ON i.id = c.item_id
		 WHERE i.list_id = ?
		 ORDER BY c.item_id, c.claimer`,
		list.ID,
	)
	if err != nil {
		return err
	}
	for claimRows.Next() {
		var (
			itemID int64
			c      claim
		)
		if err := claimRows.Scan(&itemID, &c.Claimer, &c.ClaimStart, &c.ClaimEnd); err != nil {
			claimRows.Close()
			return err
		}
		if idx, ok := indexByItemID[itemID]; ok {
			list.Items[idx].Claims = append(list.Items[idx].Claims, c)
		}
	}
	if err := claimRows.Err(); err != nil {
		claimRows.Close()
		return err
	}
	claimRows.Close()

	return nil
}

// writeLists persists the supplied lists and records a change-tree node for
// every list whose state actually changed. Lists that are present in the
// database but absent from the slice are soft-deleted: their rows and history
// remain, but they disappear from normal reads.
func writeLists(lists []itemList, meta changeMeta) error {
	for i := range lists {
		normalizeList(&lists[i])
	}
	if meta.Actor == "" {
		meta.Actor = "system"
	}
	if meta.Kind == "" {
		meta.Kind = "update"
	}
	if meta.Summary == "" {
		meta.Summary = "Updated list"
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	existing, err := readListsQ(tx, true)
	if err != nil {
		return err
	}
	existingByID := make(map[string]itemList, len(existing))
	for _, list := range existing {
		existingByID[list.ID] = list
	}

	incomingByID := make(map[string]itemList, len(lists))
	for _, list := range lists {
		incomingByID[list.ID] = list
	}

	now := nowRFC3339()
	for _, old := range existing {
		next, ok := incomingByID[old.ID]
		if !ok {
			if old.Deleted {
				continue
			}
			old.Deleted = true
			old.UpdatedAt = now
			if err := saveList(tx, old); err != nil {
				return err
			}
			if err := appendChange(tx, old, old.HeadChangeID, changeMeta{
				Actor:   meta.Actor,
				Kind:    "list_deleted",
				Summary: fmt.Sprintf("Deleted list %q", old.Name),
			}); err != nil {
				return err
			}
			continue
		}
		delete(incomingByID, old.ID)
		if listStatesEqual(old, next) {
			continue
		}
		if err := saveList(tx, next); err != nil {
			return err
		}
		if err := appendChange(tx, next, old.HeadChangeID, meta); err != nil {
			return err
		}
	}

	for _, added := range incomingByID {
		if err := saveList(tx, added); err != nil {
			return err
		}
		if err := appendChange(tx, added, 0, meta); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func listStatesEqual(a, b itemList) bool {
	a.HeadChangeID = 0
	b.HeadChangeID = 0
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

func saveList(tx *sql.Tx, list itemList) error {
	deleted := 0
	if list.Deleted {
		deleted = 1
	}
	_, err := tx.Exec(
		`INSERT INTO lists(id, name, owner_username, created_at, updated_at, deleted)
		 VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name = excluded.name,
		   owner_username = excluded.owner_username,
		   created_at = excluded.created_at,
		   updated_at = excluded.updated_at,
		   deleted = excluded.deleted`,
		list.ID,
		list.Name,
		list.OwnerUsername,
		list.CreatedAt,
		list.UpdatedAt,
		deleted,
	)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM list_collaborators WHERE list_id = ?`, list.ID); err != nil {
		return err
	}
	for _, collaborator := range list.Collaborators {
		if _, err := tx.Exec(
			`INSERT INTO list_collaborators(list_id, username) VALUES(?, ?)`,
			list.ID,
			collaborator,
		); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`DELETE FROM list_invites WHERE list_id = ?`, list.ID); err != nil {
		return err
	}
	for _, invite := range list.InviteCodes {
		if _, err := tx.Exec(
			`INSERT INTO list_invites(list_id, code, created_at, created_by) VALUES(?, ?, ?, ?)`,
			list.ID,
			invite.Code,
			invite.CreatedAt,
			invite.CreatedBy,
		); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`DELETE FROM items WHERE list_id = ?`, list.ID); err != nil {
		return err
	}
	for position, it := range list.Items {
		result, err := tx.Exec(
			`INSERT INTO items(list_id, name, target, gathered, position) VALUES(?, ?, ?, ?, ?)`,
			list.ID,
			it.Name,
			it.Target,
			it.Gathered,
			position,
		)
		if err != nil {
			return err
		}
		itemID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		for _, contribution := range it.Contributions {
			if _, err := tx.Exec(
				`INSERT INTO item_contributions(item_id, username, amount) VALUES(?, ?, ?)`,
				itemID,
				contribution.Username,
				contribution.Amount,
			); err != nil {
				return err
			}
		}
		for _, claim := range it.Claims {
			if _, err := tx.Exec(
				`INSERT INTO item_claims(item_id, claimer, claim_start, claim_end) VALUES(?, ?, ?, ?)`,
				itemID,
				claim.Claimer,
				claim.ClaimStart,
				claim.ClaimEnd,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

func appendChange(tx *sql.Tx, list itemList, parentID int64, meta changeMeta) error {
	snapshot, err := json.Marshal(list)
	if err != nil {
		return err
	}
	var parent any
	if parentID > 0 {
		parent = parentID
	}

	result, err := tx.Exec(
		`INSERT INTO change_nodes(list_id, parent_id, actor, kind, summary, snapshot, created_at, prunable)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		list.ID,
		parent,
		meta.Actor,
		meta.Kind,
		meta.Summary,
		string(snapshot),
		nowRFC3339(),
		boolToInt(changeKindPrunable(meta.Kind)),
	)
	if err != nil {
		return err
	}
	changeID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE lists SET head_change_id = ? WHERE id = ?`, changeID, list.ID)
	return err
}

func changeKindPrunable(kind string) bool {
	return kind == "gathered_updated"
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
