package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"code.sirenko.ca/grocer/internal/receipt"
	"code.sirenko.ca/grocer/internal/store"
)


type Router struct {
	store  *store.Store
	parser *receipt.Parser
	mux    *http.ServeMux
}

func NewRouter(store *store.Store, parser *receipt.Parser) *Router {
	r := &Router{
		store:  store,
		parser: parser,
		mux:    http.NewServeMux(),
	}

	r.setupRoutes()
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *Router) setupRoutes() {
	// Auth
	r.mux.HandleFunc("POST /api/auth/login", r.handleLogin)

	// Receipts
	r.mux.HandleFunc("GET /api/receipts", r.withAuth(r.handleListReceipts))
	r.mux.HandleFunc("GET /api/receipts/{id}", r.withAuth(r.handleGetReceipt))
	r.mux.HandleFunc("POST /api/receipts/upload", r.withAuth(r.handleUploadReceipt))

	// Proposals
	r.mux.HandleFunc("GET /api/proposals", r.withAuth(r.handleListProposals))
	r.mux.HandleFunc("GET /api/proposals/{id}", r.withAuth(r.handleGetProposal))
	r.mux.HandleFunc("POST /api/proposals/{id}/approve", r.withAuth(r.handleApproveProposal))

	// Items
	r.mux.HandleFunc("GET /api/items", r.withAuth(r.handleListItems))
	r.mux.HandleFunc("GET /api/items/{id}", r.withAuth(r.handleGetItem))
	r.mux.HandleFunc("PATCH /api/items/{id}", r.withAuth(r.handleUpdateItem))

	// Categories
	r.mux.HandleFunc("GET /api/categories", r.withAuth(r.handleListCategories))
	r.mux.HandleFunc("POST /api/categories", r.withAuth(r.handleCreateCategory))
	r.mux.HandleFunc("PATCH /api/categories/{id}", r.withAuth(r.handleUpdateCategory))
	r.mux.HandleFunc("DELETE /api/categories/{id}", r.withAuth(r.handleDeleteCategory))

	// Merchants
	r.mux.HandleFunc("GET /api/merchants", r.withAuth(r.handleListMerchants))
	r.mux.HandleFunc("POST /api/merchants", r.withAuth(r.handleCreateMerchant))
	r.mux.HandleFunc("PATCH /api/merchants/{id}", r.withAuth(r.handleUpdateMerchant))

	// Analysis
	r.mux.HandleFunc("GET /api/analysis/spending", r.withAuth(r.handleAnalysisSpending))
	r.mux.HandleFunc("GET /api/analysis/categories", r.withAuth(r.handleAnalysisCategories))
	r.mux.HandleFunc("GET /api/analysis/family", r.withAuth(r.handleAnalysisFamily))
	r.mux.HandleFunc("GET /api/analysis/items/{id}", r.withAuth(r.handleAnalysisItem))
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

		// Verify token hash
		match, err := verifyPassword(tokenStr, session.TokenHash)
		if err != nil || !match {
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
