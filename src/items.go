package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

const legacyContributionUser = "_legacy"

func listsCollectionHandler(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value(usernameKey).(string)
	user, err := findUser(username)
	if err != nil || user == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		lists, err := readLists()
		if err != nil {
			http.Error(w, `{"error":"Could not read lists"}`, http.StatusInternalServerError)
			return
		}

		scopeAll := r.URL.Query().Get("scope") == "all"
		if scopeAll && !user.Admin {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}

		summaries := make([]listSummary, 0, len(lists))
		for _, list := range lists {
			if !scopeAll && !canAccessList(list, username, user.Admin) {
				continue
			}
			summaries = append(summaries, makeListSummary(list, username, user.Admin))
		}
		writeJSON(w, summaries)

	case http.MethodPost:
		var req createListRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			http.Error(w, `{"error":"List name is required"}`, http.StatusBadRequest)
			return
		}

		lists, err := readLists()
		if err != nil {
			http.Error(w, `{"error":"Could not read lists"}`, http.StatusInternalServerError)
			return
		}

		newList := itemList{
			ID:            newID("list"),
			Name:          req.Name,
			OwnerUsername: username,
			Collaborators: []string{},
			InviteCodes:   []inviteCode{},
			Items:         []item{},
			CreatedAt:     nowRFC3339(),
			UpdatedAt:     nowRFC3339(),
		}
		lists = append(lists, newList)
		if err := writeLists(lists); err != nil {
			http.Error(w, `{"error":"Could not create list"}`, http.StatusInternalServerError)
			return
		}

		writeJSON(w, map[string]any{
			"success": true,
			"list":    makeListSummary(newList, username, user.Admin),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func joinListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req joinListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
		return
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		http.Error(w, `{"error":"Invite code is required"}`, http.StatusBadRequest)
		return
	}

	username := r.Context().Value(usernameKey).(string)
	user, err := findUser(username)
	if err != nil || user == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	lists, err := readLists()
	if err != nil {
		http.Error(w, `{"error":"Could not read lists"}`, http.StatusInternalServerError)
		return
	}

	for i := range lists {
		for _, invite := range lists[i].InviteCodes {
			if invite.Code != req.Code {
				continue
			}

			if lists[i].OwnerUsername == username || slices.Contains(lists[i].Collaborators, username) {
				writeJSON(w, map[string]any{
					"success": true,
					"list":    makeListSummary(lists[i], username, user.Admin),
				})
				return
			}

			lists[i].Collaborators = append(lists[i].Collaborators, username)
			touchList(&lists[i])
			if err := writeLists(lists); err != nil {
				http.Error(w, `{"error":"Could not join list"}`, http.StatusInternalServerError)
				return
			}

			writeJSON(w, map[string]any{
				"success": true,
				"list":    makeListSummary(lists[i], username, user.Admin),
			})
			return
		}
	}

	http.Error(w, `{"error":"Invite code not found"}`, http.StatusNotFound)
}

func listResourceHandler(b *sseBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/itemchecklist"), "/api/lists/")
		path = strings.Trim(path, "/")
		if path == "" {
			http.NotFound(w, r)
			return
		}

		parts := strings.Split(path, "/")
		listID := parts[0]
		username := r.Context().Value(usernameKey).(string)
		user, err := findUser(username)
		if err != nil || user == nil {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		if len(parts) == 1 {
			handleListRoot(w, r, listID, user)
			return
		}

		switch parts[1] {
		case "items":
			handleListItems(w, r, b, listID, user, parts[2:])
		case "invites":
			handleListInvites(w, r, listID, user)
		case "members":
			handleListMembers(w, r, listID, user)
		default:
			http.NotFound(w, r)
		}
	}
}

func handleListRoot(w http.ResponseWriter, r *http.Request, listID string, user *user) {
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

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, makeListDetail(lists[idx], user.Username, user.Admin))

	case http.MethodPatch:
		if !canManageList(lists[idx], user.Username, user.Admin) {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}

		var req updateListRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}

		switch req.Action {
		case "", "rename":
			req.Name = strings.TrimSpace(req.Name)
			if req.Name == "" {
				http.Error(w, `{"error":"List name is required"}`, http.StatusBadRequest)
				return
			}
			lists[idx].Name = req.Name
			touchList(&lists[idx])
		case "transfer_owner":
			if !user.Admin {
				http.Error(w, `{"error":"Only admins can transfer list ownership"}`, http.StatusForbidden)
				return
			}
			target := strings.TrimSpace(req.TransferOwner)
			targetUser, err := findUser(target)
			if err != nil {
				http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
				return
			}
			if targetUser == nil {
				http.Error(w, `{"error":"Target user not found"}`, http.StatusNotFound)
				return
			}
			lists[idx].OwnerUsername = targetUser.Username
			lists[idx].Collaborators = removeString(lists[idx].Collaborators, targetUser.Username)
			touchList(&lists[idx])
		default:
			http.Error(w, `{"error":"Unknown action"}`, http.StatusBadRequest)
			return
		}

		if err := writeLists(lists); err != nil {
			http.Error(w, `{"error":"Could not update list"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"success": true,
			"list":    makeListDetail(lists[idx], user.Username, user.Admin),
		})

	case http.MethodDelete:
		if !canManageList(lists[idx], user.Username, user.Admin) {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		lists = append(lists[:idx], lists[idx+1:]...)
		if err := writeLists(lists); err != nil {
			http.Error(w, `{"error":"Could not delete list"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleListItems(w http.ResponseWriter, r *http.Request, b *sseBroker, listID string, user *user, tail []string) {
	lists, idx, err := readListsWithIndex(listID)
	if err != nil {
		http.Error(w, `{"error":"Could not read lists"}`, http.StatusInternalServerError)
		return
	}
	if idx == -1 {
		http.Error(w, `{"error":"List not found"}`, http.StatusNotFound)
		return
	}
	list := &lists[idx]

	if !canAccessList(*list, user.Username, user.Admin) {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	if len(tail) == 0 {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, list.Items)
		case http.MethodPost:
			if !canManageList(*list, user.Username, user.Admin) {
				http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
				return
			}

			var req upsertItemRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
				return
			}
			if err := upsertManagedItem(list, req); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
				return
			}
			if err := writeLists(lists); err != nil {
				http.Error(w, `{"error":"Could not save item"}`, http.StatusInternalServerError)
				return
			}
			broadcastListUpdate(b, *list)
			writeJSON(w, map[string]bool{"success": true})

		case http.MethodDelete:
			if !canManageList(*list, user.Username, user.Admin) {
				http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
				return
			}

			var req deleteItemRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
				return
			}
			if !deleteManagedItem(list, req.Name) {
				http.Error(w, `{"error":"Item not found"}`, http.StatusNotFound)
				return
			}
			if err := writeLists(lists); err != nil {
				http.Error(w, `{"error":"Could not delete item"}`, http.StatusInternalServerError)
				return
			}
			broadcastListUpdate(b, *list)
			writeJSON(w, map[string]bool{"success": true})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if user.Frozen {
		http.Error(w, `{"error":"Your account is frozen and cannot contribute until an admin unfreezes it"}`, http.StatusForbidden)
		return
	}

	switch tail[0] {
	case "update":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req updateItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}
		if !updateListItemContribution(list, user.Username, req) {
			http.Error(w, `{"error":"Item not found"}`, http.StatusNotFound)
			return
		}
		if err := writeLists(lists); err != nil {
			http.Error(w, `{"error":"Could not save item"}`, http.StatusInternalServerError)
			return
		}
		broadcastListUpdate(b, *list)
		writeJSON(w, map[string]bool{"success": true})

	case "claim":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req claimItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}
		if !updateListItemClaim(list, user.Username, req) {
			http.Error(w, `{"error":"Item not found"}`, http.StatusNotFound)
			return
		}
		if err := writeLists(lists); err != nil {
			http.Error(w, `{"error":"Could not save item"}`, http.StatusInternalServerError)
			return
		}
		broadcastListUpdate(b, *list)
		writeJSON(w, map[string]bool{"success": true})

	default:
		http.NotFound(w, r)
	}
}

func handleListInvites(w http.ResponseWriter, r *http.Request, listID string, user *user) {
	lists, idx, err := readListsWithIndex(listID)
	if err != nil {
		http.Error(w, `{"error":"Could not read lists"}`, http.StatusInternalServerError)
		return
	}
	if idx == -1 {
		http.Error(w, `{"error":"List not found"}`, http.StatusNotFound)
		return
	}
	if !canManageList(lists[idx], user.Username, user.Admin) {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodPost:
		code, err := newInviteCode()
		if err != nil {
			http.Error(w, `{"error":"Could not create invite code"}`, http.StatusInternalServerError)
			return
		}
		lists[idx].InviteCodes = append(lists[idx].InviteCodes, inviteCode{
			Code:      code,
			CreatedAt: nowRFC3339(),
			CreatedBy: user.Username,
		})
		touchList(&lists[idx])
		if err := writeLists(lists); err != nil {
			http.Error(w, `{"error":"Could not save invite code"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"success": true,
			"list":    makeListDetail(lists[idx], user.Username, user.Admin),
		})

	case http.MethodDelete:
		var req inviteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}
		before := len(lists[idx].InviteCodes)
		filtered := lists[idx].InviteCodes[:0]
		for _, invite := range lists[idx].InviteCodes {
			if invite.Code != req.Code {
				filtered = append(filtered, invite)
			}
		}
		lists[idx].InviteCodes = filtered
		if len(lists[idx].InviteCodes) == before {
			http.Error(w, `{"error":"Invite code not found"}`, http.StatusNotFound)
			return
		}
		touchList(&lists[idx])
		if err := writeLists(lists); err != nil {
			http.Error(w, `{"error":"Could not revoke invite code"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleListMembers(w http.ResponseWriter, r *http.Request, listID string, user *user) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lists, idx, err := readListsWithIndex(listID)
	if err != nil {
		http.Error(w, `{"error":"Could not read lists"}`, http.StatusInternalServerError)
		return
	}
	if idx == -1 {
		http.Error(w, `{"error":"List not found"}`, http.StatusNotFound)
		return
	}
	if !canManageList(lists[idx], user.Username, user.Admin) {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	var req memberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
		return
	}

	removed := len(lists[idx].Collaborators) != len(removeString(lists[idx].Collaborators, req.Username))
	lists[idx].Collaborators = removeString(lists[idx].Collaborators, req.Username)
	if !removed {
		http.Error(w, `{"error":"Member not found"}`, http.StatusNotFound)
		return
	}
	touchList(&lists[idx])
	if err := writeLists(lists); err != nil {
		http.Error(w, `{"error":"Could not remove member"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"success": true})
}

func makeListSummary(list itemList, username string, isAdmin bool) listSummary {
	return listSummary{
		ID:            list.ID,
		Name:          list.Name,
		OwnerUsername: list.OwnerUsername,
		Role:          listRole(list, username, isAdmin),
		CanManage:     canManageList(list, username, isAdmin),
		ItemCount:     len(list.Items),
		Collaborators: cloneStrings(list.Collaborators),
		UpdatedAt:     list.UpdatedAt,
	}
}

func makeListDetail(list itemList, username string, isAdmin bool) listDetail {
	return listDetail{
		ID:            list.ID,
		Name:          list.Name,
		OwnerUsername: list.OwnerUsername,
		Role:          listRole(list, username, isAdmin),
		CanManage:     canManageList(list, username, isAdmin),
		Collaborators: cloneStrings(list.Collaborators),
		InviteCodes:   cloneInviteCodes(list.InviteCodes),
		Items:         cloneItems(list.Items),
		CreatedAt:     list.CreatedAt,
		UpdatedAt:     list.UpdatedAt,
	}
}

func listRole(list itemList, username string, isAdmin bool) string {
	switch {
	case isAdmin:
		return "admin"
	case list.OwnerUsername == username:
		return "owner"
	case slices.Contains(list.Collaborators, username):
		return "collaborator"
	default:
		return "viewer"
	}
}

func canAccessList(list itemList, username string, isAdmin bool) bool {
	return isAdmin || list.OwnerUsername == username || slices.Contains(list.Collaborators, username)
}

func canManageList(list itemList, username string, isAdmin bool) bool {
	return isAdmin || list.OwnerUsername == username
}

func readLists() ([]itemList, error) {
	var lists []itemList
	if _, err := os.Stat(listsPath); os.IsNotExist(err) {
		return []itemList{}, nil
	}
	if err := readJSONFile(listsPath, &lists); err != nil {
		return nil, err
	}

	changed := false
	for i := range lists {
		if normalizeList(&lists[i]) {
			changed = true
		}
	}
	if changed {
		if err := writeJSONFile(listsPath, lists); err != nil {
			return nil, err
		}
	}
	return lists, nil
}

func writeLists(lists []itemList) error {
	for i := range lists {
		normalizeList(&lists[i])
	}
	return writeJSONFile(listsPath, lists)
}

func readListsWithIndex(listID string) ([]itemList, int, error) {
	lists, err := readLists()
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

func readLegacyItems() ([]item, error) {
	var items []item
	if _, err := os.Stat(itemsPath); os.IsNotExist(err) {
		return []item{}, nil
	}
	if err := readJSONFile(itemsPath, &items); err != nil {
		return nil, err
	}
	for i := range items {
		normalizeItem(&items[i])
	}
	return items, nil
}

func normalizeList(list *itemList) bool {
	changed := false

	if list.CreatedAt == "" {
		list.CreatedAt = nowRFC3339()
		changed = true
	}
	if list.UpdatedAt == "" {
		list.UpdatedAt = list.CreatedAt
		changed = true
	}
	if list.Collaborators == nil {
		list.Collaborators = []string{}
		changed = true
	}
	if list.InviteCodes == nil {
		list.InviteCodes = []inviteCode{}
		changed = true
	}
	if list.Items == nil {
		list.Items = []item{}
		changed = true
	}

	seenUsers := map[string]bool{}
	collaborators := list.Collaborators[:0]
	for _, collaborator := range list.Collaborators {
		collaborator = strings.TrimSpace(collaborator)
		if collaborator == "" || collaborator == list.OwnerUsername || seenUsers[collaborator] {
			changed = true
			continue
		}
		seenUsers[collaborator] = true
		collaborators = append(collaborators, collaborator)
	}
	list.Collaborators = collaborators

	for i := range list.Items {
		if normalizeItem(&list.Items[i]) {
			changed = true
		}
	}

	return changed
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

	if it.Target < 0 {
		it.Target = 0
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
	if it.Gathered > it.Target && it.Target > 0 {
		it.Gathered = it.Target
		trimContributionsToTarget(it)
		changed = true
	}

	var claims []claim
	for _, c := range it.Claims {
		if c.Claimer == "" || c.ClaimEnd <= c.ClaimStart {
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

func trimContributionsToTarget(it *item) {
	remaining := it.Target
	updated := make([]contribution, 0, len(it.Contributions))
	for _, c := range it.Contributions {
		if remaining <= 0 {
			break
		}
		if c.Amount > remaining {
			c.Amount = remaining
		}
		updated = append(updated, c)
		remaining -= c.Amount
	}
	it.Contributions = updated
	total := 0
	for _, c := range updated {
		total += c.Amount
	}
	it.Gathered = total
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
	if it.Gathered < 0 {
		it.Gathered = 0
	}
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

func revokeUserEditsFromLists(lists []itemList, username string) bool {
	changed := false
	for i := range lists {
		listChanged := false
		for j := range lists[i].Items {
			if removeContributionByUsername(&lists[i].Items[j], username) > 0 {
				changed = true
				listChanged = true
			}
			beforeClaims := len(lists[i].Items[j].Claims)
			removeClaimByName(&lists[i].Items[j], username)
			if beforeClaims != len(lists[i].Items[j].Claims) {
				changed = true
				listChanged = true
			}
		}
		if lists[i].OwnerUsername == username {
			continue
		}
		updatedCollaborators := removeString(lists[i].Collaborators, username)
		if len(updatedCollaborators) != len(lists[i].Collaborators) {
			lists[i].Collaborators = updatedCollaborators
			changed = true
			listChanged = true
		}
		if listChanged {
			touchList(&lists[i])
		}
	}
	return changed
}

func deleteListsOwnedByUser(lists []itemList, username string) ([]itemList, int) {
	filtered := make([]itemList, 0, len(lists))
	deleted := 0
	for _, list := range lists {
		if list.OwnerUsername == username {
			deleted++
			continue
		}
		filtered = append(filtered, list)
	}
	return filtered, deleted
}

func updateListItemContribution(list *itemList, username string, req updateItemRequest) bool {
	for i := range list.Items {
		if list.Items[i].Name != req.Name {
			continue
		}
		delta := req.Delta
		if delta == 0 {
			delta = req.Gathered - list.Items[i].Gathered
		}
		applyContributionDelta(&list.Items[i], username, delta)
		touchList(list)
		return true
	}
	return false
}

func updateListItemClaim(list *itemList, username string, req claimItemRequest) bool {
	for i := range list.Items {
		if list.Items[i].Name != req.Name {
			continue
		}
		if req.Claimed < 0 {
			req.Claimed = 0
		}
		remaining := list.Items[i].Target - list.Items[i].Gathered
		if remaining < 0 {
			remaining = 0
		}
		if req.Claimed > remaining {
			req.Claimed = remaining
		}

		if req.Claimed == 0 {
			removeClaimByName(&list.Items[i], username)
		} else {
			existingClaim := getClaimByName(&list.Items[i], username)
			if existingClaim == nil {
				list.Items[i].Claims = append(list.Items[i].Claims, claim{
					Claimer:    username,
					ClaimStart: list.Items[i].Gathered,
					ClaimEnd:   list.Items[i].Gathered + req.Claimed,
				})
			} else {
				existingClaim.ClaimStart = list.Items[i].Gathered
				existingClaim.ClaimEnd = list.Items[i].Gathered + req.Claimed
			}
		}
		touchList(list)
		return true
	}
	return false
}

func upsertManagedItem(list *itemList, req upsertItemRequest) error {
	name := strings.TrimSpace(req.Name)
	originalName := strings.TrimSpace(req.OriginalName)
	if name == "" {
		return fmt.Errorf("item name is required")
	}
	if req.Target < 0 {
		return fmt.Errorf("item target must be zero or greater")
	}

	for i := range list.Items {
		if list.Items[i].Name != originalName && strings.EqualFold(list.Items[i].Name, name) {
			return fmt.Errorf("an item with that name already exists")
		}
	}

	for i := range list.Items {
		if list.Items[i].Name != originalName {
			continue
		}
		list.Items[i].Name = name
		list.Items[i].Target = req.Target
		normalizeItem(&list.Items[i])
		touchList(list)
		return nil
	}

	list.Items = append(list.Items, item{
		Name:          name,
		Target:        req.Target,
		Gathered:      0,
		Claims:        []claim{},
		Contributions: []contribution{},
	})
	touchList(list)
	return nil
}

func deleteManagedItem(list *itemList, name string) bool {
	name = strings.TrimSpace(name)
	for i := range list.Items {
		if list.Items[i].Name != name {
			continue
		}
		list.Items = append(list.Items[:i], list.Items[i+1:]...)
		touchList(list)
		return true
	}
	return false
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func touchList(list *itemList) {
	list.UpdatedAt = nowRFC3339()
}

func removeString(values []string, target string) []string {
	filtered := values[:0]
	for _, value := range values {
		if value != target {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func newInviteCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string{}, values...)
}

func cloneInviteCodes(values []inviteCode) []inviteCode {
	if len(values) == 0 {
		return []inviteCode{}
	}
	return append([]inviteCode{}, values...)
}

func cloneItems(values []item) []item {
	if len(values) == 0 {
		return []item{}
	}
	return append([]item{}, values...)
}
