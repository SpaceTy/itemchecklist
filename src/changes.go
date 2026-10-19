package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// prunableChangeRetention controls how long individual "amount" changes are
// kept as distinct nodes. Older ones are folded away, but the newest node of
// each list and every structural change are always retained.
const prunableChangeRetention = 7 * 24 * time.Hour

var errChangeNotFound = errors.New("change not found")

type changeNode struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parent_id"`
	Actor     string `json:"actor"`
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"created_at"`
	Prunable  bool   `json:"prunable"`
	Current   bool   `json:"current"`
}

type changeDetail struct {
	changeNode
	Snapshot itemList `json:"snapshot"`
}

func scheduleChangePruning() {
	pruneOldAmountChanges()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		pruneOldAmountChanges()
	}
}

func pruneOldAmountChanges() {
	if err := pruneChanges(time.Now().UTC()); err != nil {
		log.Printf("change tree pruning error: %v", err)
	}
}

func listHistory(listID string, limit int, beforeID int64) ([]changeNode, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	query := `SELECT id, parent_id, actor, kind, summary, created_at, prunable
	          FROM change_nodes WHERE list_id = ?`
	args := []any{listID}
	if beforeID > 0 {
		query += ` AND id < ?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}

	nodes := []changeNode{}
	for rows.Next() {
		var node changeNode
		var parent sql.NullInt64
		var prunable int
		if err := rows.Scan(
			&node.ID,
			&parent,
			&node.Actor,
			&node.Kind,
			&node.Summary,
			&node.CreatedAt,
			&prunable,
		); err != nil {
			rows.Close()
			return nil, err
		}
		if parent.Valid {
			value := parent.Int64
			node.ParentID = &value
		}
		node.Prunable = prunable != 0
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var headID sql.NullInt64
	if err := db.QueryRow(`SELECT head_change_id FROM lists WHERE id = ?`, listID).Scan(&headID); err == nil && headID.Valid {
		for i := range nodes {
			if nodes[i].ID == headID.Int64 {
				nodes[i].Current = true
			}
		}
	}

	return nodes, nil
}

func changeWithSnapshot(listID string, changeID int64) (*changeDetail, error) {
	detail := &changeDetail{}
	var parent sql.NullInt64
	var prunable int
	var snapshot string

	err := db.QueryRow(
		`SELECT id, parent_id, actor, kind, summary, created_at, prunable, snapshot
		 FROM change_nodes WHERE id = ? AND list_id = ?`,
		changeID,
		listID,
	).Scan(
		&detail.ID,
		&parent,
		&detail.Actor,
		&detail.Kind,
		&detail.Summary,
		&detail.CreatedAt,
		&prunable,
		&snapshot,
	)
	if err == sql.ErrNoRows {
		return nil, errChangeNotFound
	}
	if err != nil {
		return nil, err
	}
	if parent.Valid {
		value := parent.Int64
		detail.ParentID = &value
	}
	detail.Prunable = prunable != 0
	if err := json.Unmarshal([]byte(snapshot), &detail.Snapshot); err != nil {
		return nil, err
	}
	return detail, nil
}

// rollbackList materializes the snapshot stored on changeID and records the
// restoration as a new node whose parent is the change that was restored,
// leaving the original branch intact.
func rollbackList(listID string, changeID int64, actor string) (*itemList, error) {
	detail, err := changeWithSnapshot(listID, changeID)
	if err != nil {
		return nil, err
	}

	restored := detail.Snapshot
	restored.ID = listID
	normalizeList(&restored)
	restored.UpdatedAt = nowRFC3339()

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := saveList(tx, restored); err != nil {
		return nil, err
	}

	snapshot, err := json.Marshal(restored)
	if err != nil {
		return nil, err
	}
	result, err := tx.Exec(
		`INSERT INTO change_nodes(list_id, parent_id, actor, kind, summary, snapshot, created_at, prunable)
		 VALUES(?, ?, ?, ?, ?, ?, ?, 0)`,
		listID,
		changeID,
		actor,
		"rollback",
		fmt.Sprintf("Rolled back to change #%d", changeID),
		string(snapshot),
		nowRFC3339(),
	)
	if err != nil {
		return nil, err
	}
	newChangeID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE lists SET head_change_id = ? WHERE id = ?`, newChangeID, listID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	restored.HeadChangeID = newChangeID
	return &restored, nil
}

func pruneChanges(now time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	heads := map[string]int64{}
	headRows, err := tx.Query(`SELECT id, COALESCE(head_change_id, 0) FROM lists`)
	if err != nil {
		return err
	}
	for headRows.Next() {
		var (
			listID string
			headID int64
		)
		if err := headRows.Scan(&listID, &headID); err != nil {
			headRows.Close()
			return err
		}
		heads[listID] = headID
	}
	if err := headRows.Err(); err != nil {
		headRows.Close()
		return err
	}
	headRows.Close()

	type nodeInfo struct {
		id        int64
		listID    string
		parentID  *int64
		createdAt time.Time
		prunable  bool
	}

	nodes := []nodeInfo{}
	nodeRows, err := tx.Query(`SELECT id, list_id, parent_id, created_at, prunable FROM change_nodes ORDER BY id`)
	if err != nil {
		return err
	}
	for nodeRows.Next() {
		var (
			node     nodeInfo
			parent   sql.NullInt64
			prunable int
			created  string
		)
		if err := nodeRows.Scan(&node.id, &node.listID, &parent, &created, &prunable); err != nil {
			nodeRows.Close()
			return err
		}
		if parent.Valid {
			value := parent.Int64
			node.parentID = &value
		}
		node.prunable = prunable != 0
		node.createdAt, _ = time.Parse(time.RFC3339, created)
		nodes = append(nodes, node)
	}
	if err := nodeRows.Err(); err != nil {
		nodeRows.Close()
		return err
	}
	nodeRows.Close()

	parents := make(map[int64]*int64, len(nodes))
	toDelete := make(map[int64]bool)
	cutoff := now.Add(-prunableChangeRetention)
	for _, node := range nodes {
		parents[node.id] = node.parentID
		if !node.prunable {
			continue
		}
		if heads[node.listID] == node.id {
			continue
		}
		if node.createdAt.IsZero() || node.createdAt.Before(cutoff) {
			toDelete[node.id] = true
		}
	}

	if len(toDelete) == 0 {
		return tx.Commit()
	}

	for _, node := range nodes {
		if !toDelete[node.id] {
			continue
		}
		parent := node.parentID
		for parent != nil && toDelete[*parent] {
			parent = parents[*parent]
		}
		if _, err := tx.Exec(`UPDATE change_nodes SET parent_id = ? WHERE parent_id = ?`, parent, node.id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM change_nodes WHERE id = ?`, node.id); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func handleListHistory(w http.ResponseWriter, r *http.Request, b *sseBroker, listID string, user *user, tail []string) {
	lists, idx, err := readListsWithIndex(listID)
	if err != nil {
		http.Error(w, `{"error":"Could not read lists"}`, http.StatusInternalServerError)
		return
	}
	if idx == -1 {
		http.Error(w, `{"error":"List not found"}`, http.StatusNotFound)
		return
	}
	if !canAccessList(lists[idx], user.Username, user.Admin) {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	if len(tail) == 0 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}
		beforeID := int64(0)
		if raw := r.URL.Query().Get("before"); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				beforeID = parsed
			}
		}
		nodes, err := listHistory(listID, limit, beforeID)
		if err != nil {
			http.Error(w, `{"error":"Could not read history"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, nodes)
		return
	}

	changeID, err := strconv.ParseInt(tail[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		detail, err := changeWithSnapshot(listID, changeID)
		if err == errChangeNotFound {
			http.Error(w, `{"error":"Change not found"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"Could not read change"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, detail)

	case http.MethodPost:
		if !canManageList(lists[idx], user.Username, user.Admin) {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		restored, err := rollbackList(listID, changeID, user.Username)
		if err == errChangeNotFound {
			http.Error(w, `{"error":"Change not found"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"Could not roll back"}`, http.StatusInternalServerError)
			return
		}
		broadcastListUpdate(b, *restored)
		writeJSON(w, map[string]any{
			"success": true,
			"list":    makeListDetail(*restored, user.Username, user.Admin),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
