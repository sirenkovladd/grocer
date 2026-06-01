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

	router := NewRouter(s, nil, nil, nil)
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
