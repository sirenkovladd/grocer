package bot

import (
	"context"
)

type Bot interface {
	Start(ctx context.Context) error
	Stop() error
}

type ReceiptHandler interface {
	HandlePhoto(ctx context.Context, photo []byte, senderID string) (uint64, error)
}
