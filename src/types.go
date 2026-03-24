package main

type user struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	Admin        bool   `json:"admin"`
	Frozen       bool   `json:"frozen"`
}

type userInfo struct {
	Username string `json:"username"`
	Admin    bool   `json:"admin"`
	Frozen   bool   `json:"frozen"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type adminUserRequest struct {
	Username string `json:"username"`
	Action   string `json:"action"`
}

type adminSettingsRequest struct {
	RegistrationLockedDown bool `json:"registration_locked_down"`
}

type appSettings struct {
	RegistrationLockedDown bool `json:"registration_locked_down"`
}

type item struct {
	Name          string         `json:"name"`
	Target        int            `json:"target"`
	Gathered      int            `json:"gathered"`
	Claims        []claim        `json:"claims"`
	Contributions []contribution `json:"contributions,omitempty"`
}

type claim struct {
	Claimer    string `json:"claimer"`
	ClaimStart int    `json:"claim_start"`
	ClaimEnd   int    `json:"claim_end"`
}

type contribution struct {
	Username string `json:"username"`
	Amount   int    `json:"amount"`
}

type inviteCode struct {
	Code      string `json:"code"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
}

type itemList struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	OwnerUsername string       `json:"owner_username"`
	Collaborators []string     `json:"collaborators"`
	InviteCodes   []inviteCode `json:"invite_codes"`
	Items         []item       `json:"items"`
	CreatedAt     string       `json:"created_at"`
	UpdatedAt     string       `json:"updated_at"`
}

type listSummary struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	OwnerUsername string   `json:"owner_username"`
	Role          string   `json:"role"`
	CanManage     bool     `json:"can_manage"`
	ItemCount     int      `json:"item_count"`
	Collaborators []string `json:"collaborators"`
	UpdatedAt     string   `json:"updated_at"`
}

type listDetail struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	OwnerUsername string       `json:"owner_username"`
	Role          string       `json:"role"`
	CanManage     bool         `json:"can_manage"`
	Collaborators []string     `json:"collaborators"`
	InviteCodes   []inviteCode `json:"invite_codes"`
	Items         []item       `json:"items"`
	CreatedAt     string       `json:"created_at"`
	UpdatedAt     string       `json:"updated_at"`
}

type createListRequest struct {
	Name string `json:"name"`
}

type updateListRequest struct {
	Name          string `json:"name"`
	Action        string `json:"action"`
	TransferOwner string `json:"transfer_owner"`
}

type joinListRequest struct {
	Code string `json:"code"`
}

type memberRequest struct {
	Username string `json:"username"`
}

type inviteRequest struct {
	Code string `json:"code"`
}

type upsertItemRequest struct {
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	Target       int    `json:"target"`
}

type deleteItemRequest struct {
	Name string `json:"name"`
}

type updateItemRequest struct {
	Name     string `json:"name"`
	Gathered int    `json:"gathered"`
	Delta    int    `json:"delta"`
}

type claimItemRequest struct {
	Name    string `json:"name"`
	Claimed int    `json:"claimed"`
}

type sseMessage struct {
	Type   string `json:"type"`
	ListID string `json:"list_id"`
	Items  []item `json:"items"`
}
