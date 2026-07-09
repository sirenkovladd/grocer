package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/store"
	"golang.org/x/crypto/argon2"
)

func setupTestRouter(t *testing.T) (*Router, *store.Store) {
	s, err := store.NewStore()
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	router := NewRouter(s, nil, nil, nil, nil)
	return router, s
}

func TestHealthEndpoint(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", response["status"])
	}
}

func TestCORSHeaders(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("OPTIONS", "/api/auth/login", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for OPTIONS, got %d", w.Code)
	}

	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("Expected CORS headers to be set")
	}

	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Expected Access-Control-Allow-Methods header to be set")
	}
}

func TestLoginEndpoint(t *testing.T) {
	router, s := setupTestRouter(t)

	// Create a test user
	user := &domain.User{
		UserID:       s.UserID.Gen(),
		Username:     "testuser",
		Name:         "Test User",
		PasswordHash: hashPasswordForTest("password123"),
	}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Test login
	loginReq := map[string]string{
		"username": "testuser",
		"password": "password123",
	}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["token"] == nil || response["token"] == "" {
		t.Error("Expected token in response")
	}
}

func TestLoginRateLimiting(t *testing.T) {
	router, s := setupTestRouter(t)

	// Create a test user
	user := &domain.User{
		UserID:       s.UserID.Gen(),
		Username:     "testuser",
		Name:         "Test User",
		PasswordHash: hashPasswordForTest("password123"),
	}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	loginReq := map[string]string{
		"username": "testuser",
		"password": "wrongpassword",
	}
	body, _ := json.Marshal(loginReq)

	// Make 6 requests (limit is 5 per minute)
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if i < 5 {
			// First 5 requests should fail with 401 (wrong password)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Request %d: Expected status 401, got %d", i, w.Code)
			}
		} else {
			// 6th request should fail with 429 (rate limited)
			if w.Code != http.StatusTooManyRequests {
				t.Errorf("Request %d: Expected status 429, got %d", i, w.Code)
			}
		}
	}
}

func TestInputValidation(t *testing.T) {
	router, s := setupTestRouter(t)

	// Create a test user and get token
	user := &domain.User{
		UserID:       s.UserID.Gen(),
		Username:     "testuser",
		Name:         "Test User",
		PasswordHash: hashPasswordForTest("password123"),
	}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	token := createTestSession(t, s, user.UserID)

	// Test creating category with empty name
	categoryReq := map[string]string{
		"name": "",
	}
	body, _ := json.Marshal(categoryReq)

	req := httptest.NewRequest("POST", "/api/categories", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for empty name, got %d", w.Code)
	}

	// Test creating category with valid name
	categoryReq["name"] = "Valid Category"
	body, _ = json.Marshal(categoryReq)

	req = httptest.NewRequest("POST", "/api/categories", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201 for valid category, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestUnauthorizedAccess(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/receipts", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestAuthorizedAccess(t *testing.T) {
	router, s := setupTestRouter(t)

	// Create a test user and session
	user := &domain.User{
		UserID:       s.UserID.Gen(),
		Username:     "testuser",
		Name:         "Test User",
		PasswordHash: hashPasswordForTest("password123"),
	}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	token := createTestSession(t, s, user.UserID)

	req := httptest.NewRequest("GET", "/api/receipts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestSessionCookie(t *testing.T) {
	router, s := setupTestRouter(t)

	// Override COOKIE_SECURE so the test isn't affected by the dev shell.
	t.Setenv("COOKIE_SECURE", "false")

	user := &domain.User{
		UserID:       s.UserID.Gen(),
		Username:     "cookietest",
		Name:         "Cookie Test",
		PasswordHash: hashPasswordForTest("password123"),
	}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	loginReq := map[string]string{
		"username": "cookietest",
		"password": "password123",
	}
	body, _ := json.Marshal(loginReq)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Find the Set-Cookie: session=... entry and verify its flags.
	cookies := w.Result().Cookies()
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == "session" {
			session = c
			break
		}
	}
	if session == nil {
		t.Fatal("Expected Set-Cookie: session=...; got none")
	}
	if !session.HttpOnly {
		t.Error("Expected HttpOnly=true")
	}
	if session.SameSite != http.SameSiteLaxMode {
		t.Errorf("Expected SameSite=Lax, got %v", session.SameSite)
	}
	if session.Path != "/" {
		t.Errorf("Expected Path=/, got %q", session.Path)
	}
	if session.MaxAge != 60*60*24*7 {
		t.Errorf("Expected MaxAge=7 days, got %d", session.MaxAge)
	}
	if session.Secure {
		t.Error("Expected Secure=false (COOKIE_SECURE=false in this test)")
	}
	if session.Value == "" {
		t.Error("Expected non-empty cookie value")
	}

	// Now use the cookie to make an authenticated request.
	req2 := httptest.NewRequest("GET", "/api/receipts", nil)
	req2.AddCookie(session)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Cookie-authenticated request failed: %d %s", w2.Code, w2.Body.String())
	}
}

func TestSessionCookieRejectedWithoutCookieOrHeader(t *testing.T) {
	router, _ := setupTestRouter(t)

	// No cookie, no header → 401.
	req := httptest.NewRequest("GET", "/api/receipts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestLogoutClearsCookieAndSession(t *testing.T) {
	router, s := setupTestRouter(t)
	t.Setenv("COOKIE_SECURE", "false")

	user := &domain.User{
		UserID:       s.UserID.Gen(),
		Username:     "logouttest",
		Name:         "Logout Test",
		PasswordHash: hashPasswordForTest("password123"),
	}
	if err := s.CreateUser(user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Log in to create a session + cookie.
	loginReq := map[string]string{
		"username": "logouttest",
		"password": "password123",
	}
	body, _ := json.Marshal(loginReq)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", w.Code)
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			session = c
			break
		}
	}
	if session == nil {
		t.Fatal("expected session cookie after login")
	}
	sessionID, _, err := parseTokenString(session.Value)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if _, err := s.GetSession(sessionID); err != nil {
		t.Fatalf("expected session to exist after login, got %v", err)
	}

	// Logout with the cookie.
	logoutReq := httptest.NewRequest("POST", "/api/auth/logout", nil)
	logoutReq.AddCookie(session)
	logoutW := httptest.NewRecorder()
	router.ServeHTTP(logoutW, logoutReq)

	if logoutW.Code != http.StatusOK {
		t.Errorf("logout: expected 200, got %d", logoutW.Code)
	}
	// The response must include a Set-Cookie: session=...; Max-Age=-1
	// (or Expires in the past) to clear the browser's copy.
	cleared := false
	for _, c := range logoutW.Result().Cookies() {
		if c.Name == "session" && (c.MaxAge < 0 || c.Value == "") {
			cleared = true
			break
		}
	}
	if !cleared {
		t.Error("expected logout to clear the session cookie")
	}
	if _, err := s.GetSession(sessionID); err == nil {
		t.Error("expected session to be deleted from the store after logout")
	}

	// Logout is idempotent — calling again with no cookie still returns 200.
	logoutReq2 := httptest.NewRequest("POST", "/api/auth/logout", nil)
	logoutW2 := httptest.NewRecorder()
	router.ServeHTTP(logoutW2, logoutReq2)
	if logoutW2.Code != http.StatusOK {
		t.Errorf("second logout: expected 200, got %d", logoutW2.Code)
	}
}

// Helper functions

func hashPasswordForTest(password string) string {
	// Use argon2 for testing (same as production)
	salt := []byte("testsalt12345678")
	hash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)

	saltB64 := base64.RawStdEncoding.EncodeToString(salt)
	hashB64 := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=19$m=65536,t=1,p=4$%s$%s", saltB64, hashB64)
}

func createTestSession(t *testing.T, s *store.Store, userID uint64) string {
	sessionID := s.SessionID.Gen()
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	tokenStr := base64.RawStdEncoding.EncodeToString(tokenBytes)
	tokenHash := hashSessionToken(tokenStr)

	session := &store.Session{
		SessionID: sessionID,
		TokenHash: tokenHash,
		UserID:    userID,
	}

	if err := s.CreateSession(session); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	return fmt.Sprintf("%d:%s", sessionID, tokenStr)
}
