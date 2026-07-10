package store

import (
	"encoding/json"
	"fmt"
	"os"
)

// projectIDFromCredentialsFile reads the project_id field from a
// Google Cloud service account JSON file. Used to fall back to a
// credentials-file-derived project ID when GCP_PROJECT_ID is not
// explicitly set.
func projectIDFromCredentialsFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	// Only decode the field we need. The full service account JSON
	// has dozens of fields we don't care about.
	var creds struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	if creds.ProjectID == "" {
		return "", fmt.Errorf("%s: no project_id field", path)
	}
	return creds.ProjectID, nil
}
