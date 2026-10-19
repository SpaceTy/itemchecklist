package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
)

func adminUsersHandler(b *sseBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			users, err := readUsers()
			if err != nil {
				http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
				return
			}
			info := make([]userInfo, len(users))
			for i, u := range users {
				info[i] = userInfo{Username: u.Username, Admin: u.Admin, Frozen: u.Frozen}
			}
			writeJSON(w, info)

		case http.MethodPost:
			var req adminUserRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
				return
			}

			callerUsername := r.Context().Value(usernameKey).(string)

			users, err := readUsers()
			if err != nil {
				http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
				return
			}

			idx := -1
			for i, u := range users {
				if u.Username == req.Username {
					idx = i
					break
				}
			}
			if idx == -1 {
				http.Error(w, `{"error":"User not found"}`, http.StatusNotFound)
				return
			}

			switch req.Action {
			case "toggle_frozen":
				users[idx].Frozen = !users[idx].Frozen

			case "purge_progress":
				lists, err := readLists()
				if err != nil {
					http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
					return
				}
				changed := revokeUserEditsFromLists(lists, req.Username)
				if changed {
					if err := writeLists(lists, changeMeta{
						Actor:   callerUsername,
						Kind:    "user_edits_revoked",
						Summary: fmt.Sprintf("Removed all edits by %q", req.Username),
					}); err != nil {
						http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
						return
					}
					broadcastAllLists(b, lists)
				}
				writeJSON(w, map[string]bool{"success": true})
				return

			case "delete":
				if users[idx].Username == callerUsername {
					http.Error(w, `{"error":"Cannot delete your own account"}`, http.StatusBadRequest)
					return
				}

				lists, err := readLists()
				if err != nil {
					http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
					return
				}
				revokeUserEditsFromLists(lists, req.Username)
				lists, _ = deleteListsOwnedByUser(lists, req.Username)
				if err := writeLists(lists, changeMeta{
					Actor:   callerUsername,
					Kind:    "user_deleted",
					Summary: fmt.Sprintf("Removed user %q and their lists", req.Username),
				}); err != nil {
					http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
					return
				}
				broadcastAllLists(b, lists)
				users = append(users[:idx], users[idx+1:]...)

			case "toggle_admin":
				users[idx].Admin = !users[idx].Admin

			default:
				http.Error(w, `{"error":"Unknown action"}`, http.StatusBadRequest)
				return
			}

			if err := writeUsers(users); err != nil {
				http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]bool{"success": true})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func readUsers() ([]user, error) {
	rows, err := db.Query(`SELECT username, password_hash, admin, frozen FROM users WHERE deleted = 0 ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []user{}
	for rows.Next() {
		var (
			u             user
			admin, frozen int
		)
		if err := rows.Scan(&u.Username, &u.PasswordHash, &admin, &frozen); err != nil {
			return nil, err
		}
		u.Admin = admin != 0
		u.Frozen = frozen != 0
		users = append(users, u)
	}
	return users, rows.Err()
}

func writeUsers(users []user) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT username FROM users WHERE deleted = 0`)
	if err != nil {
		return err
	}
	existing := []string{}
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, username)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	keep := make(map[string]bool, len(users))
	for _, u := range users {
		keep[u.Username] = true
	}
	for _, username := range existing {
		if keep[username] {
			continue
		}
		if _, err := tx.Exec(`UPDATE users SET deleted = 1 WHERE username = ?`, username); err != nil {
			return err
		}
	}

	for _, u := range users {
		if _, err := tx.Exec(
			`INSERT INTO users(username, password_hash, admin, frozen, deleted)
			 VALUES(?, ?, ?, ?, 0)
			 ON CONFLICT(username) DO UPDATE SET
			   password_hash = excluded.password_hash,
			   admin = excluded.admin,
			   frozen = excluded.frozen,
			   deleted = 0`,
			u.Username,
			u.PasswordHash,
			boolToInt(u.Admin),
			boolToInt(u.Frozen),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func findUser(username string) (*user, error) {
	users, err := readUsers()
	if err != nil {
		return nil, err
	}
	for i := range users {
		if users[i].Username == username {
			return &users[i], nil
		}
	}
	return nil, nil
}

func adminSettingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := readSettings()
		if err != nil {
			http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, settings)

	case http.MethodPost:
		var req adminSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}
		settings := appSettings{
			RegistrationLockedDown: req.RegistrationLockedDown,
		}
		if err := writeSettings(settings); err != nil {
			http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func readSettings() (appSettings, error) {
	var settings appSettings
	var locked int
	err := db.QueryRow(`SELECT registration_locked_down FROM settings WHERE id = 1`).Scan(&locked)
	if err == sql.ErrNoRows {
		return appSettings{}, nil
	}
	if err != nil {
		return appSettings{}, err
	}
	settings.RegistrationLockedDown = locked != 0
	return settings, nil
}

func writeSettings(settings appSettings) error {
	_, err := db.Exec(
		`INSERT INTO settings(id, registration_locked_down) VALUES(1, ?)
		 ON CONFLICT(id) DO UPDATE SET registration_locked_down = excluded.registration_locked_down`,
		boolToInt(settings.RegistrationLockedDown),
	)
	return err
}
