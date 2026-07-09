package receipt

// Proposal status values. The lifecycle is:
//
//	uploaded → parsed_ocr → parsed_llm → pending → approved
//	    │           │            │
//	    └───────────┴────────────┴──→ failed
//
// `pending` is the "ready for user review" terminal state.
// `failed` is retryable via the reparse endpoint.
const (
	StatusUploaded  = "uploaded"
	StatusParsedOCR = "parsed_ocr"
	StatusParsedLLM = "parsed_llm"
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusFailed    = "failed"
)

// IsInProgress reports whether the proposal is still being parsed and
// therefore a stream consumer should keep listening for events.
func IsInProgress(status string) bool {
	switch status {
	case StatusUploaded, StatusParsedOCR, StatusParsedLLM, "parsing":
		return true
	}
	return false
}
