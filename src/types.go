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
	Type  string `json:"type"`
	Items []item `json:"items"`
}
