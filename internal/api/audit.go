package api

import (
	"log"
	"net/http"
	"time"
)

// AuditLog represents an audit log entry
type AuditLog struct {
	Timestamp time.Time `json:"timestamp"`
	UserID    uint64    `json:"userId,omitempty"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	ResourceID string   `json:"resourceId,omitempty"`
	IP        string    `json:"ip"`
	Success   bool      `json:"success"`
	Message   string    `json:"message,omitempty"`
}

// auditLogger logs audit events
type auditLogger struct {
	enabled bool
}

var globalAuditLogger = &auditLogger{
	enabled: true, // Can be controlled via environment variable
}

// withAuditLogging adds audit logging to API endpoints
func (r *Router) withAuditLogging(action, resource string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		
		// Create a response writer wrapper to capture status code
		ww := &auditResponseWriter{
			ResponseWriter: w,
			statusCode:     200,
		}

		// Extract user ID if available (after auth middleware)
		var userID uint64
		if id, ok := req.Context().Value(userIDKey).(uint64); ok {
			userID = id
		}

		// Extract resource ID from path if available
		resourceID := req.PathValue("id")

		// Call the next handler
		next.ServeHTTP(ww, req)

		// Log the audit event
		if globalAuditLogger.enabled {
			entry := AuditLog{
				Timestamp:  start,
				UserID:     userID,
				Action:     action,
				Resource:   resource,
				ResourceID: resourceID,
				IP:         getClientIP(req),
				Success:    ww.statusCode >= 200 && ww.statusCode < 400,
			}

			if !entry.Success {
				entry.Message = http.StatusText(ww.statusCode)
			}

			log.Printf("AUDIT: user=%d action=%s resource=%s id=%s ip=%s success=%v status=%d duration=%v",
				entry.UserID,
				entry.Action,
				entry.Resource,
				entry.ResourceID,
				entry.IP,
				entry.Success,
				ww.statusCode,
				time.Since(start))
		}
	}
}

// auditResponseWriter wraps http.ResponseWriter to capture status code
type auditResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *auditResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// logAuditEvent logs a specific audit event (can be called from handlers)
func logAuditEvent(userID uint64, action, resource, resourceID, ip string, success bool, message string) {
	if !globalAuditLogger.enabled {
		return
	}

	entry := AuditLog{
		Timestamp:  time.Now(),
		UserID:     userID,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		IP:         ip,
		Success:    success,
		Message:    message,
	}

	log.Printf("AUDIT: user=%d action=%s resource=%s id=%s ip=%s success=%v message=%s",
		entry.UserID,
		entry.Action,
		entry.Resource,
		entry.ResourceID,
		entry.IP,
		entry.Success,
		entry.Message)
}
