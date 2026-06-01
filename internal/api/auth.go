package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"code.sirenko.ca/grocer/internal/store"
	"golang.org/x/crypto/argon2"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string      `json:"token"`
	User  interface{} `json:"user"`
}

func (r *Router) handleLogin(w http.ResponseWriter, req *http.Request) {
	var reqBody loginRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := r.store.GetUserByUsername(reqBody.Username)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Verify password
	match, err := verifyPassword(reqBody.Password, user.PasswordHash)
	if err != nil || !match {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Cleanup all existing sessions for this user
	if err := r.store.DeleteSessionsByUserID(user.UserID); err != nil {
		log.Printf("Warning: Failed to cleanup sessions for user %d: %v", user.UserID, err)
	}

	// Create session token (32 random bytes)
	token, err := generateRandomBytes(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tokenString := base64.RawStdEncoding.EncodeToString(token)

	// Hash token with HMAC-SHA256 for storage (fast verification)
	tokenHash := hashSessionToken(tokenString)

	session := &store.Session{
		SessionID: r.store.SessionID.Gen(),
		TokenHash: tokenHash,
		UserID:    user.UserID,
	}

	if err := r.store.CreateSession(session); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	idTokenString := fmt.Sprintf("%d:%s", session.SessionID, tokenString)

	writeJSON(w, http.StatusOK, loginResponse{
		Token: idTokenString,
		User:  user,
	})
}

func verifyPassword(password, encodedHash string) (bool, error) {
	vals := strings.Split(encodedHash, "$")
	if len(vals) != 6 {
		return false, fmt.Errorf("invalid hash format")
	}

	var version int
	_, err := fmt.Sscanf(vals[2], "v=%d", &version)
	if err != nil {
		return false, err
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	_, err = fmt.Sscanf(vals[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism)
	if err != nil {
		return false, err
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(vals[4])
	if err != nil {
		return false, err
	}

	hash, err := base64.RawStdEncoding.Strict().DecodeString(vals[5])
	if err != nil {
		return false, err
	}

	otherHash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(hash)))

	return subtle.ConstantTimeCompare(hash, otherHash) == 1, nil
}

// hashSessionToken creates a SHA-256 hash of the session token.
// The token is already 32 bytes of cryptographic randomness,
// so plain SHA-256 is sufficient for constant-time comparison.
func hashSessionToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return base64.RawStdEncoding.EncodeToString(h[:])
}

// verifySessionToken verifies a session token against its HMAC hash.
func verifySessionToken(token, hash string) bool {
	expected := hashSessionToken(token)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(hash)) == 1
}

func generateRandomBytes(n uint32) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

// ParseTokenString moved here to avoid circular dependency
func parseTokenString(tokenString string) (uint64, string, error) {
	vals := strings.Split(tokenString, ":")
	if len(vals) != 2 {
		return 0, "", fmt.Errorf("invalid token string")
	}
	id, err := strconv.ParseUint(vals[0], 10, 64)
	if err != nil {
		return 0, "", err
	}
	return id, vals[1], nil
}
