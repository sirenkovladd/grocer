package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"code.sirenko.ca/grocer/internal/events"
	"code.sirenko.ca/grocer/internal/photo"
	"code.sirenko.ca/grocer/internal/receipt"
	"code.sirenko.ca/grocer/internal/store"
)

type Router struct {
	store      *store.Store
	parser     *receipt.Parser
	photoStore photo.Store
	photoCache *photo.LocalCache
	mux        *http.ServeMux
	eventHub   *events.Hub
}

func NewRouter(store *store.Store, parser *receipt.Parser, photoStore photo.Store, photoCache *photo.LocalCache, eventHub *events.Hub) *Router {
	r := &Router{
		store:      store,
		parser:     parser,
		photoStore: photoStore,
		photoCache: photoCache,
		mux:        http.NewServeMux(),
		eventHub:   eventHub,
	}

	r.setupRoutes()
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Handle CORS preflight requests at the top level
	if req.Method == "OPTIONS" && strings.HasPrefix(req.URL.Path, "/api/") {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.WriteHeader(http.StatusOK)
		return
	}
	
	r.mux.ServeHTTP(w, req)
}

func (r *Router) setupRoutes() {
	// Health check
	r.mux.HandleFunc("GET /api/health", r.withCORS(r.handleHealth))

	// Auth (with rate limiting)
	r.mux.HandleFunc("POST /api/auth/login", r.withCORS(r.withRateLimit(r.handleLogin, 5, time.Minute)))

	// Receipts
	r.mux.HandleFunc("GET /api/receipts", r.withCORS(r.withAuth(r.withAuditLogging("list", "receipts", r.handleListReceipts))))
	r.mux.HandleFunc("GET /api/receipts/{id}", r.withCORS(r.withAuth(r.withAuditLogging("read", "receipt", r.handleGetReceipt))))
	r.mux.HandleFunc("POST /api/receipts/upload", r.withCORS(r.withAuth(r.withAuditLogging("upload", "receipt", r.handleUploadReceipt))))

	// Proposals
	r.mux.HandleFunc("GET /api/proposals", r.withCORS(r.withAuth(r.withAuditLogging("list", "proposals", r.handleListProposals))))
	r.mux.HandleFunc("GET /api/proposals/{id}", r.withCORS(r.withAuth(r.withAuditLogging("read", "proposal", r.handleGetProposal))))
	r.mux.HandleFunc("GET /api/proposals/{id}/stream", r.withCORS(r.withAuth(r.handleProposalStream)))
	r.mux.HandleFunc("POST /api/proposals/{id}/approve", r.withCORS(r.withAuth(r.withAuditLogging("approve", "proposal", r.handleApproveProposal))))
	r.mux.HandleFunc("POST /api/proposals/{id}/reparse", r.withCORS(r.withAuth(r.withAuditLogging("reparse", "proposal", r.handleReparseProposal))))
	r.mux.HandleFunc("DELETE /api/proposals/{id}", r.withCORS(r.withAuth(r.withAuditLogging("delete", "proposal", r.handleDeleteProposal))))
	r.mux.HandleFunc("PATCH /api/proposals/{id}/items/{index}", r.withCORS(r.withAuth(r.withAuditLogging("update", "proposal_item", r.handleUpdateProposalItem))))

	// Items
	r.mux.HandleFunc("GET /api/items", r.withCORS(r.withAuth(r.withAuditLogging("list", "items", r.handleListItems))))
	r.mux.HandleFunc("GET /api/items/{id}", r.withCORS(r.withAuth(r.withAuditLogging("read", "item", r.handleGetItem))))
	r.mux.HandleFunc("PATCH /api/items/{id}", r.withCORS(r.withAuth(r.withAuditLogging("update", "item", r.handleUpdateItem))))

	// Categories
	r.mux.HandleFunc("GET /api/categories", r.withCORS(r.withAuth(r.withAuditLogging("list", "categories", r.handleListCategories))))
	r.mux.HandleFunc("POST /api/categories", r.withCORS(r.withAuth(r.withAuditLogging("create", "category", r.handleCreateCategory))))
	r.mux.HandleFunc("PATCH /api/categories/{id}", r.withCORS(r.withAuth(r.withAuditLogging("update", "category", r.handleUpdateCategory))))
	r.mux.HandleFunc("DELETE /api/categories/{id}", r.withCORS(r.withAuth(r.withAuditLogging("delete", "category", r.handleDeleteCategory))))

	// Merchants
	r.mux.HandleFunc("GET /api/merchants", r.withCORS(r.withAuth(r.withAuditLogging("list", "merchants", r.handleListMerchants))))
	r.mux.HandleFunc("POST /api/merchants", r.withCORS(r.withAuth(r.withAuditLogging("create", "merchant", r.handleCreateMerchant))))
	r.mux.HandleFunc("PATCH /api/merchants/{id}", r.withCORS(r.withAuth(r.withAuditLogging("update", "merchant", r.handleUpdateMerchant))))

	// Analysis
	r.mux.HandleFunc("GET /api/analysis/spending", r.withCORS(r.withAuth(r.withAuditLogging("read", "analysis_spending", r.handleAnalysisSpending))))
	r.mux.HandleFunc("GET /api/analysis/categories", r.withCORS(r.withAuth(r.withAuditLogging("read", "analysis_categories", r.handleAnalysisCategories))))
	r.mux.HandleFunc("GET /api/analysis/family", r.withCORS(r.withAuth(r.withAuditLogging("read", "analysis_family", r.handleAnalysisFamily))))
	r.mux.HandleFunc("GET /api/analysis/items/{id}", r.withCORS(r.withAuth(r.withAuditLogging("read", "analysis_item", r.handleAnalysisItem))))

	// Photos
	r.mux.HandleFunc("GET /api/photos/{id}", r.withCORS(r.withAuth(r.handleGetPhoto)))

	// Search
	r.mux.HandleFunc("GET /api/search/receipts", r.withCORS(r.withAuth(r.withAuditLogging("search", "receipts", r.handleSearchReceipts))))
	r.mux.HandleFunc("GET /api/search/items", r.withCORS(r.withAuth(r.withAuditLogging("search", "items", r.handleSearchItems))))

	// Serve static files from dist/ — SPA fallback
	r.mux.Handle("GET /", r.serveSPA())
}

type contextKey string

const userIDKey contextKey = "userID"

func (r *Router) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		authHeader := req.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			writeError(w, http.StatusUnauthorized, "invalid authorization format")
			return
		}

		sessionID, tokenStr, err := parseTokenString(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		session, err := r.store.GetSession(sessionID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid session")
			return
		}

		// Verify token hash using fast HMAC-SHA256
		if !verifySessionToken(tokenStr, session.TokenHash) {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		ctx := context.WithValue(req.Context(), userIDKey, session.UserID)
		next(w, req.WithContext(ctx))
	}
}

func (r *Router) getUserID(req *http.Request) uint64 {
	return req.Context().Value(userIDKey).(uint64)
}

// withCORS adds CORS headers to the response
func (r *Router) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if req.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, req)
	}
}

// Rate limiter
type rateLimiter struct {
	requests map[string][]time.Time
	mu       sync.Mutex
}

var globalRateLimiter = &rateLimiter{
	requests: make(map[string][]time.Time),
}

// withRateLimit limits requests per IP address
func (r *Router) withRateLimit(next http.HandlerFunc, maxRequests int, window time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ip := getClientIP(req)
		
		globalRateLimiter.mu.Lock()
		defer globalRateLimiter.mu.Unlock()

		now := time.Now()
		windowStart := now.Add(-window)

		// Clean old requests
		var validRequests []time.Time
		for _, t := range globalRateLimiter.requests[ip] {
			if t.After(windowStart) {
				validRequests = append(validRequests, t)
			}
		}
		globalRateLimiter.requests[ip] = validRequests

		// Check rate limit
		if len(validRequests) >= maxRequests {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		// Add current request
		globalRateLimiter.requests[ip] = append(globalRateLimiter.requests[ip], now)

		next(w, req)
	}
}

// getClientIP extracts the client IP address from the request
func getClientIP(req *http.Request) string {
	// Check X-Forwarded-For header (for proxies)
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := req.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return ip
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": "1.0.0",
	})
}

// serveSPA serves static files from dist/ with SPA fallback to index.html.
func (r *Router) serveSPA() http.Handler {
	distDir := "dist"

	// Check if dist/ exists, try common locations
	for _, dir := range []string{"dist", "../dist", "../../dist"} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			distDir = dir
			break
		}
	}

	fs := http.Dir(distDir)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Try to serve the requested file
		path := req.URL.Path
		if path == "/" {
			path = "index.html"
		}

		// Clean the path
		path = filepath.Clean(path)
		if path == "." {
			path = "index.html"
		}

		// Try opening the file
		f, err := fs.Open(path)
		if err != nil {
			// File not found — serve index.html (SPA fallback)
			req.URL.Path = "/"
			http.FileServer(fs).ServeHTTP(w, req)
			return
		}
		f.Close()

		// File exists — serve it directly
		http.FileServer(fs).ServeHTTP(w, req)
	})
}
