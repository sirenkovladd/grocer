package database

import (
	"code.sirenko.ca/grocer/lib/database/out_proto"
	"github.com/hashicorp/go-memdb"
)

type Session struct {
	SessionId uint64
	TokenHash string
	User      *out_proto.User
}

var genUserId = NewGenerator()
var genSessionId = NewGenerator()

func getSchema() *memdb.DBSchema {
	return &memdb.DBSchema{
		Tables: map[string]*memdb.TableSchema{
			"users": {
				Name: "users",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:   "id",
						Unique: true,
						Indexer: &memdb.StringFieldIndex{
							Field: "Username",
						},
					},
				},
			},
			"sessions": {
				Name: "sessions",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:   "id",
						Unique: true,
						Indexer: &memdb.UintFieldIndex{
							Field: "SessionId",
						},
					},
				},
			},
		},
	}
}

func getClient() (*memdb.MemDB, error) {
	return memdb.NewMemDB(getSchema())
}
