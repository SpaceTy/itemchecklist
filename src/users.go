package main

import (
	"encoding/json"
	"net/http"
	"os"
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
				items, err := readItems()
				if err != nil {
					http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
					return
				}
				if revokeUserEdits(items, req.Username) {
					if err := writeItems(items); err != nil {
						http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
						return
					}
					broadcastItemsUpdate(b, items)
				}
				writeJSON(w, map[string]bool{"success": true})
				return
			case "delete":
				if users[idx].Username == callerUsername {
					http.Error(w, `{"error":"Cannot delete your own account"}`, http.StatusBadRequest)
					return
				}
				items, err := readItems()
				if err != nil {
					http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
					return
				}
				itemsChanged := revokeUserEdits(items, req.Username)
				users = append(users[:idx], users[idx+1:]...)
				if itemsChanged {
					if err := writeItems(items); err != nil {
						http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
						return
					}
					broadcastItemsUpdate(b, items)
				}
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
	var users []user
	if _, err := os.Stat(usersPath); os.IsNotExist(err) {
		return []user{}, nil
	}
	if err := readJSONFile(usersPath, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func writeUsers(users []user) error {
	return writeJSONFile(usersPath, users)
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
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		return appSettings{}, nil
	}
	if err := readJSONFile(settingsPath, &settings); err != nil {
		return appSettings{}, err
	}
	return settings, nil
}

func writeSettings(settings appSettings) error {
	return writeJSONFile(settingsPath, settings)
}
