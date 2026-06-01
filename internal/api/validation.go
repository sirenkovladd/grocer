package api

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	maxNameLength     = 100
	maxAliasesCount   = 20
	maxAliasLength    = 100
)

// validateCategoryName validates category names
func validateCategoryName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("category name cannot be empty")
	}
	if len(name) > maxNameLength {
		return fmt.Errorf("category name too long (max %d characters)", maxNameLength)
	}
	if !isValidName(name) {
		return fmt.Errorf("category name contains invalid characters")
	}
	return nil
}

// validateMerchantName validates merchant names
func validateMerchantName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("merchant name cannot be empty")
	}
	if len(name) > maxNameLength {
		return fmt.Errorf("merchant name too long (max %d characters)", maxNameLength)
	}
	if !isValidName(name) {
		return fmt.Errorf("merchant name contains invalid characters")
	}
	return nil
}

// validateItemName validates item names
func validateItemName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("item name cannot be empty")
	}
	if len(name) > maxNameLength {
		return fmt.Errorf("item name too long (max %d characters)", maxNameLength)
	}
	if !isValidName(name) {
		return fmt.Errorf("item name contains invalid characters")
	}
	return nil
}

// validateAliases validates a list of aliases
func validateAliases(aliases []string) error {
	if len(aliases) > maxAliasesCount {
		return fmt.Errorf("too many aliases (max %d)", maxAliasesCount)
	}
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			return fmt.Errorf("alias cannot be empty")
		}
		if len(alias) > maxAliasLength {
			return fmt.Errorf("alias too long (max %d characters)", maxAliasLength)
		}
	}
	return nil
}

// validatePrice validates price in cents (must be non-negative)
func validatePrice(cents int64) error {
	if cents < 0 {
		return fmt.Errorf("price cannot be negative")
	}
	if cents > 10000000 { // $100,000 max
		return fmt.Errorf("price too high")
	}
	return nil
}

// isValidName checks if a name contains only valid characters
// Allows letters, numbers, spaces, and common punctuation
func isValidName(name string) bool {
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsSpace(r) &&
			r != '-' && r != '\'' && r != '&' && r != '.' && r != ',' && r != '/' && r != '(' && r != ')' {
			return false
		}
	}
	return true
}
