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
	"os"
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

// sessionCookieName is the name of the session cookie set on login. It is
// short and unprefixed because Path: "/" scopes it to the whole origin and
// the app is single-domain.
const sessionCookieName = "session"

// sessionCookieMaxAge matches the maximum lifetime of a session cookie.
// The store itself does not currently expire sessions, so this is the
// only bound on session lifetime for browser users.
const sessionCookieMaxAge = 60 * 60 * 24 * 7 // 7 days

// cookieSecure reports whether the session cookie should be marked
// Secure. Defaults to true; set COOKIE_SECURE=false in local dev (plain
// HTTP) to allow the browser to send the cookie back.
func cookieSecure() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("COOKIE_SECURE")))
	if v == "" {
		return true
	}
	return v != "false" && v != "0" && v != "no"
}

// setSessionCookie writes a Set-Cookie header that authenticates the
// caller for subsequent requests. The browser auto-attaches it for
// same-origin requests, so no client-side token storage is needed.
func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionCookieMaxAge,
	})
}

// clearSessionCookie instructs the browser to discard the session cookie.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
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

	// Set the HttpOnly session cookie. The JSON token field is kept for
	// non-browser clients (tests, future CLI tools) but the browser no
	// longer stores it client-side.
	setSessionCookie(w, idTokenString)

	writeJSON(w, http.StatusOK, loginResponse{
		Token: idTokenString,
		User:  user,
	})
}

// handleLogout invalidates the current session and instructs the browser
// to drop the session cookie. Idempotent: returns 200 even if there is no
// valid session, so callers don't have to special-case "already logged
// out".
func (r *Router) handleLogout(w http.ResponseWriter, req *http.Request) {
	// Try to delete the session if the caller has one, but don't error
	// out if the token is missing or invalid — the goal is to clear
	// local state either way.
	if c, err := req.Cookie(sessionCookieName); err == nil && c.Value != "" {
		if sessionID, _, perr := parseTokenString(c.Value); perr == nil {
			if err := r.store.DeleteSession(sessionID); err != nil {
				log.Printf("Warning: failed to delete session %d: %v", sessionID, err)
			}
		}
	} else if authHeader := req.Header.Get("Authorization"); authHeader != "" {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != authHeader {
			if sessionID, _, perr := parseTokenString(token); perr == nil {
				if err := r.store.DeleteSession(sessionID); err != nil {
					log.Printf("Warning: failed to delete session %d: %v", sessionID, err)
				}
			}
		}
	}

	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
