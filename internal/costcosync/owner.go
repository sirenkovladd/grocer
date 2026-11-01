package costcosync

import (
	"fmt"
	"sort"
	"strings"

	"code.sirenko.ca/grocer/internal/store"
)

// ResolveOwner maps the --owner username flag to a user id. Empty flag
// means the single user when the store holds exactly one; otherwise it
// is an error that names the candidates so receipts are never
// misattributed by guessing.
func ResolveOwner(s *store.Store, flag string) (uint64, error) {
	if flag != "" {
		u, err := s.GetUserByUsername(flag)
		if err != nil || u == nil {
			return 0, fmt.Errorf("owner %q not found", flag)
		}
		return u.UserID, nil
	}
	users, err := s.ListUsers()
	if err != nil {
		return 0, err
	}
	if len(users) == 0 {
		return 0, fmt.Errorf("no users in store (create one first: go run ./cmd/server --create-user --name NAME --username USER --password PASS)")
	}
	if len(users) == 1 {
		return users[0].UserID, nil
	}
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Username)
	}
	sort.Strings(names)
	return 0, fmt.Errorf("multiple users, specify --owner: %s", strings.Join(names, ", "))
}
