package main

import (
	"encoding/json"
	"net/http"
	"os"
)

func getItemsHandler(w http.ResponseWriter, r *http.Request) {
	items, err := readItems()
	if err != nil {
		http.Error(w, `{"error":"Could not read items"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, items)
}

func updateItemHandler(b *sseBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req updateItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}

		items, err := readItems()
		if err != nil {
			http.Error(w, `{"error":"Could not read items"}`, http.StatusInternalServerError)
			return
		}

		updated := false
		for i := range items {
			if items[i].Name == req.Name {
				if req.Gathered < 0 {
					req.Gathered = 0
				}
				if req.Gathered > items[i].Target {
					req.Gathered = items[i].Target
				}
				items[i].Gathered = req.Gathered
				updated = true
				break
			}
		}

		if !updated {
			http.Error(w, `{"error":"Item not found"}`, http.StatusNotFound)
			return
		}

		if err := writeItems(items); err != nil {
			http.Error(w, `{"error":"Could not write items"}`, http.StatusInternalServerError)
			return
		}

		broadcastItemsUpdate(b, items)
		writeJSON(w, map[string]bool{"success": true})
	}
}

func claimItemHandler(b *sseBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req claimItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}

		claimer := r.Context().Value(usernameKey).(string)

		items, err := readItems()
		if err != nil {
			http.Error(w, `{"error":"Could not read items"}`, http.StatusInternalServerError)
			return
		}

		updated := false
		for i := range items {
			if items[i].Name == req.Name {
				if req.Claimed < 0 {
					req.Claimed = 0
				}
				remaining := items[i].Target - items[i].Gathered
				if remaining < 0 {
					remaining = 0
				}
				if req.Claimed > remaining {
					req.Claimed = remaining
				}

				if req.Claimed == 0 {
					removeClaimByName(&items[i], claimer)
				} else {
					existingClaim := getClaimByName(&items[i], claimer)
					if existingClaim == nil {
						items[i].Claims = append(items[i].Claims, claim{
							Claimer:    claimer,
							ClaimStart: items[i].Gathered,
							ClaimEnd:   items[i].Gathered + req.Claimed,
						})
					} else {
						existingClaim.ClaimStart = items[i].Gathered
						existingClaim.ClaimEnd = items[i].Gathered + req.Claimed
					}
				}
				updated = true
				break
			}
		}

		if !updated {
			http.Error(w, `{"error":"Item not found"}`, http.StatusNotFound)
			return
		}

		if err := writeItems(items); err != nil {
			http.Error(w, `{"error":"Could not write items"}`, http.StatusInternalServerError)
			return
		}

		broadcastItemsUpdate(b, items)
		writeJSON(w, map[string]bool{"success": true})
	}
}

func removeClaimByName(it *item, name string) {
	var newClaims []claim
	for _, c := range it.Claims {
		if c.Claimer != name {
			newClaims = append(newClaims, c)
		}
	}
	it.Claims = newClaims
}

func getClaimByName(it *item, name string) *claim {
	for i := range it.Claims {
		if it.Claims[i].Claimer == name {
			return &it.Claims[i]
		}
	}
	return nil
}

func readItems() ([]item, error) {
	var items []item
	if _, err := os.Stat(itemsPath); err != nil {
		if os.IsNotExist(err) {
			return []item{}, nil
		}
		return nil, err
	}
	if err := readJSONFile(itemsPath, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func writeItems(items []item) error {
	return writeJSONFile(itemsPath, items)
}
