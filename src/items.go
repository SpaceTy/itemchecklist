package main

import (
	"encoding/json"
	"net/http"
	"os"
)

const legacyContributionUser = "_legacy"

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

		username := r.Context().Value(usernameKey).(string)

		items, err := readItems()
		if err != nil {
			http.Error(w, `{"error":"Could not read items"}`, http.StatusInternalServerError)
			return
		}

		updated := false
		for i := range items {
			if items[i].Name == req.Name {
				delta := req.Delta
				if delta == 0 {
					delta = req.Gathered - items[i].Gathered
				}
				applyContributionDelta(&items[i], username, delta)
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
	changed := false
	for i := range items {
		if normalizeItem(&items[i]) {
			changed = true
		}
	}
	if changed {
		if err := writeJSONFile(itemsPath, items); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func writeItems(items []item) error {
	for i := range items {
		normalizeItem(&items[i])
	}
	return writeJSONFile(itemsPath, items)
}

func normalizeItem(it *item) bool {
	changed := false

	if len(it.Contributions) == 0 && it.Gathered > 0 {
		it.Contributions = []contribution{{
			Username: legacyContributionUser,
			Amount:   it.Gathered,
		}}
		changed = true
	}

	var normalized []contribution
	total := 0
	for _, c := range it.Contributions {
		if c.Username == "" || c.Amount <= 0 {
			changed = true
			continue
		}
		normalized = append(normalized, c)
		total += c.Amount
	}
	if len(normalized) != len(it.Contributions) {
		changed = true
	}
	it.Contributions = normalized

	if it.Gathered != total {
		it.Gathered = total
		changed = true
	}

	var claims []claim
	for _, c := range it.Claims {
		if c.ClaimEnd <= c.ClaimStart {
			changed = true
			continue
		}
		if c.ClaimStart < 0 {
			c.ClaimStart = 0
			changed = true
		}
		if c.ClaimEnd > it.Target {
			c.ClaimEnd = it.Target
			changed = true
		}
		if c.ClaimEnd <= c.ClaimStart {
			changed = true
			continue
		}
		claims = append(claims, c)
	}
	if len(claims) != len(it.Claims) {
		changed = true
	}
	it.Claims = claims

	return changed
}

func getContributionByUsername(it *item, username string) *contribution {
	for i := range it.Contributions {
		if it.Contributions[i].Username == username {
			return &it.Contributions[i]
		}
	}
	return nil
}

func applyContributionDelta(it *item, username string, delta int) int {
	if delta == 0 {
		return 0
	}

	normalizeItem(it)

	own := 0
	existing := getContributionByUsername(it, username)
	if existing != nil {
		own = existing.Amount
	}

	if delta < 0 && -delta > own {
		delta = -own
	}

	remaining := it.Target - it.Gathered
	if remaining < 0 {
		remaining = 0
	}
	if delta > remaining {
		delta = remaining
	}
	if delta == 0 {
		return 0
	}

	newAmount := own + delta
	if existing == nil {
		it.Contributions = append(it.Contributions, contribution{
			Username: username,
			Amount:   newAmount,
		})
	} else if newAmount == 0 {
		removeContributionByUsername(it, username)
	} else {
		existing.Amount = newAmount
	}

	it.Gathered += delta
	return delta
}

func removeContributionByUsername(it *item, username string) int {
	removed := 0
	var updated []contribution
	for _, c := range it.Contributions {
		if c.Username == username {
			removed += c.Amount
			continue
		}
		updated = append(updated, c)
	}
	if removed > 0 {
		it.Contributions = updated
		it.Gathered -= removed
		if it.Gathered < 0 {
			it.Gathered = 0
		}
	}
	return removed
}

func revokeUserEdits(items []item, username string) bool {
	changed := false
	for i := range items {
		if removeContributionByUsername(&items[i], username) > 0 {
			changed = true
		}
		beforeClaims := len(items[i].Claims)
		removeClaimByName(&items[i], username)
		if len(items[i].Claims) != beforeClaims {
			changed = true
		}
	}
	return changed
}
