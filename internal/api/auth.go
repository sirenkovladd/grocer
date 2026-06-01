package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

	// Create session
	token, err := generateRandomBytes(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tokenString := base64.RawStdEncoding.EncodeToString(token)

	tokenHash, err := generateFromPasswordShort(tokenString)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

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

func generateRandomBytes(n uint32) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

func generateFromPasswordShort(password string) (string, error) {
	salt, err := generateRandomBytes(16)
	if err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("%s$%s", b64Salt, b64Hash), nil
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
