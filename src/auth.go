package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var jwtSecret []byte

type contextKey string

const usernameKey contextKey = "username"

func loadOrCreateSecret() {
	data, err := os.ReadFile(secretPath)
	if err == nil {
		jwtSecret = bytes.TrimSpace(data)
		return
	}
	if !os.IsNotExist(err) {
		log.Fatalf("reading JWT secret: %v", err)
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("generating JWT secret: %v", err)
	}
	secret := hex.EncodeToString(b)
	if err := os.WriteFile(secretPath, []byte(secret), 0600); err != nil {
		log.Fatalf("writing JWT secret: %v", err)
	}
	jwtSecret = []byte(secret)
	log.Printf("Created JWT secret in %s", secretPath)
}

func generateJWT(username string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   username,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
}

func verifyJWT(tokenString string) (string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return "", fmt.Errorf("invalid token")
	}
	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return "", fmt.Errorf("invalid claims")
	}
	return claims.Subject, nil
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := currentUserFromRequest(r)
		if err != nil {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), usernameKey, user.Username)
		next(w, r.WithContext(ctx))
	}
}

func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, err := currentUserFromRequest(r)
		if err != nil || user == nil || !user.Admin {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func requireContributionAccess(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, err := currentUserFromRequest(r)
		if err != nil || user == nil {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if user.Frozen {
			http.Error(w, `{"error":"Your account is frozen and cannot contribute until an admin unfreezes it"}`, http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func currentUserFromRequest(r *http.Request) (*user, error) {
	cookie, err := r.Cookie("auth_token")
	if err != nil {
		return nil, err
	}
	username, err := verifyJWT(cookie.Value)
	if err != nil {
		return nil, err
	}
	return findUser(username)
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		http.Error(w, `{"error":"Username and password required"}`, http.StatusBadRequest)
		return
	}

	u, err := findUser(req.Username)
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}
	if u == nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		http.Error(w, `{"error":"Invalid username or password"}`, http.StatusUnauthorized)
		return
	}

	token, err := generateJWT(u.Username)
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}

	setAuthCookie(w, token, int(tokenTTL.Seconds()))
	writeJSON(w, map[string]any{
		"success":  true,
		"username": u.Username,
		"admin":    u.Admin,
		"frozen":   u.Frozen,
	})
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)

	if req.Username == "" {
		http.Error(w, `{"error":"Username is required"}`, http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(req.Username, " \t\n\r") {
		http.Error(w, `{"error":"Username must not contain spaces"}`, http.StatusBadRequest)
		return
	}
	if len(req.Username) > 32 {
		http.Error(w, `{"error":"Username must be 32 characters or fewer"}`, http.StatusBadRequest)
		return
	}
	if len(req.Password) < 6 {
		http.Error(w, `{"error":"Password must be at least 6 characters"}`, http.StatusBadRequest)
		return
	}

	users, err := readUsers()
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}
	for _, u := range users {
		if strings.EqualFold(u.Username, req.Username) {
			http.Error(w, `{"error":"Username already taken"}`, http.StatusConflict)
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}

	settings, err := readSettings()
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}

	newUser := user{
		Username:     req.Username,
		PasswordHash: string(hash),
		Frozen:       settings.RegistrationLockedDown,
	}
	users = append(users, newUser)
	if err := writeUsers(users); err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}

	token, err := generateJWT(newUser.Username)
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}

	setAuthCookie(w, token, int(tokenTTL.Seconds()))
	writeJSON(w, map[string]any{
		"success":  true,
		"username": newUser.Username,
		"admin":    false,
		"frozen":   newUser.Frozen,
	})
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	setAuthCookie(w, "", -1)
	writeJSON(w, map[string]bool{"success": true})
}

func checkAuthHandler(w http.ResponseWriter, r *http.Request) {
	u, err := currentUserFromRequest(r)
	if err != nil || u == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{
		"success":  true,
		"username": u.Username,
		"admin":    u.Admin,
		"frozen":   u.Frozen,
	})
}

func setAuthCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
