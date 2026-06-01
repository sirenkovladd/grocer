package database

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"code.sirenko.ca/grocer/lib/database/out_proto"
	"code.sirenko.ca/grocer/lib/encryption"
	"github.com/hashicorp/go-memdb"
)

type Client struct {
	db *memdb.MemDB
}

func NewClient() (*Client, error) {
	db, err := getClient()
	if err != nil {
		return nil, err
	}
	return &Client{
		db: db,
	}, nil
}

func (c *Client) GetUser(username string) (*out_proto.User, error) {
	txn := c.db.Txn(false)
	defer txn.Abort()

	iter, err := txn.First("users", "id", username)
	if err != nil {
		return nil, err
	}

	if iter == nil {
		return nil, nil
	}

	user := iter.(*out_proto.User)
	return user, nil
}

func (c *Client) LoginUser(username, password string) (*out_proto.User, string, error) {
	txn := c.db.Txn(false)

	iter, err := txn.First("users", "id", username)
	txn.Abort()
	if err != nil {
		return nil, "", err
	}

	if iter == nil {
		return nil, "", nil
	}

	user := iter.(*out_proto.User)
	match, err := encryption.ComparePasswordAndHash(password, user.PasswordHash)
	if err != nil {
		return nil, "", err
	}
	if !match {
		return nil, "", nil
	}
	token, err := encryption.GenerateRandomBytes(32)
	if err != nil {
		return nil, "", err
	}
	tokenString := base64.RawStdEncoding.EncodeToString(token)
	tokenHash, err := encryption.GenerateFromPasswordShort(tokenString)
	if err != nil {
		return nil, "", err
	}
	sessionId := genSessionId.Gen()
	session := Session{
		SessionId: sessionId,
		TokenHash: tokenHash,
		User:      user,
	}
	txn = c.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("sessions", &session); err != nil {
		return nil, "", err
	}

	txn.Commit()

	idTokenString := fmt.Sprintf("%d:%s", sessionId, tokenString)
	return user, idTokenString, nil
}

func ParseTokenString(tokenString string) (uint64, string, error) {
	vals := strings.Split(tokenString, ":")
	if len(vals) != 2 {
		return 0, "", errors.New("invalid token string")
	}
	id, err := strconv.ParseUint(vals[0], 10, 64)
	if err != nil {
		return 0, "", err
	}
	return id, vals[1], nil
}

func (c *Client) GetUserBySession(tokenString string) (*out_proto.User, error) {
	txn := c.db.Txn(false)
	defer txn.Abort()

	id, tokenString, err := ParseTokenString(tokenString)
	if err != nil {
		return nil, err
	}
	iter, err := txn.First("sessions", "id", id)
	if err != nil {
		return nil, err
	}

	if iter == nil {
		return nil, nil
	}

	session := iter.(*Session)
	if match, err := encryption.ComparePasswordAndHashShort(tokenString, session.TokenHash); err != nil {
		return nil, err
	} else if !match {
		return nil, nil
	}
	return session.User, nil
}

func (c *Client) CreateUser(name, username, password string) error {
	passwordHash, err := encryption.GenerateFromPassword(password)
	if err != nil {
		return err
	}
	user := &out_proto.User{
		UserId:       genUserId.Gen(),
		Name:         name,
		Username:     username,
		PasswordHash: passwordHash,
	}
	txn := c.db.Txn(true)
	defer txn.Abort()

	if err := txn.Insert("users", user); err != nil {
		return err
	}

	txn.Commit()
	return nil
}
