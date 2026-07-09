package store

import (
	"bytes"
	"compress/gzip"
	"io"

	"code.sirenko.ca/grocer/internal/domain"
	pb "code.sirenko.ca/grocer/proto/out_proto"
	"google.golang.org/protobuf/proto"
)

type SnapshotData struct {
	Users      []*domain.User
	Categories []*domain.Category
	Merchants  []*domain.Merchant
	Items      []*domain.Item
	Receipts   []*domain.Receipt
	Proposals  []*domain.Proposal
	BotUsers   []*BotUser
	Sessions   []*Session
}

func SerializeSnapshot(data *SnapshotData) ([]byte, error) {
	snapshot := &pb.Snapshot{
		Users:      usersToProto(data.Users),
		Categories: categoriesToProto(data.Categories),
		Merchants:  merchantsToProto(data.Merchants),
		Items:      itemsToProto(data.Items),
		Receipts:   receiptsToProto(data.Receipts),
		Proposals:  proposalsToProto(data.Proposals),
		BotUsers:   botUsersToProto(data.BotUsers),
		Sessions:   sessionsToProto(data.Sessions),
	}

	raw, err := proto.Marshal(snapshot)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func DeserializeSnapshot(data []byte) (*SnapshotData, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	var snapshot pb.Snapshot
	if err := proto.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}

	return &SnapshotData{
		Users:      usersFromProto(snapshot.Users),
		Categories: categoriesFromProto(snapshot.Categories),
		Merchants:  merchantsFromProto(snapshot.Merchants),
		Items:      itemsFromProto(snapshot.Items),
		Receipts:   receiptsFromProto(snapshot.Receipts),
		Proposals:  proposalsFromProto(snapshot.Proposals),
		BotUsers:   botUsersFromProto(snapshot.BotUsers),
		Sessions:   sessionsFromProto(snapshot.Sessions),
	}, nil
}

// Proto conversion functions

func usersToProto(users []*domain.User) []*pb.User {
	result := make([]*pb.User, len(users))
	for i, u := range users {
		result[i] = &pb.User{
			UserId:       u.UserID,
			Name:         u.Name,
			Username:     u.Username,
			PasswordHash: u.PasswordHash,
		}
	}
	return result
}

func usersFromProto(users []*pb.User) []*domain.User {
	result := make([]*domain.User, len(users))
	for i, u := range users {
		result[i] = &domain.User{
			UserID:       u.UserId,
			Name:         u.Name,
			Username:     u.Username,
			PasswordHash: u.PasswordHash,
		}
	}
	return result
}

func categoriesToProto(cats []*domain.Category) []*pb.Category {
	result := make([]*pb.Category, len(cats))
	for i, c := range cats {
		cat := &pb.Category{
			CategoryId: c.CategoryID,
			Name:       c.Name,
			SortOrder:  c.SortOrder,
		}
		if c.ParentID != nil {
			cat.ParentId = c.ParentID
		}
		result[i] = cat
	}
	return result
}

func categoriesFromProto(cats []*pb.Category) []*domain.Category {
	result := make([]*domain.Category, len(cats))
	for i, c := range cats {
		cat := &domain.Category{
			CategoryID: c.CategoryId,
			Name:       c.Name,
			SortOrder:  c.SortOrder,
		}
		if c.ParentId != nil {
			cat.ParentID = c.ParentId
		}
		result[i] = cat
	}
	return result
}

func merchantsToProto(merchants []*domain.Merchant) []*pb.Merchant {
	result := make([]*pb.Merchant, len(merchants))
	for i, m := range merchants {
		result[i] = &pb.Merchant{
			MerchantId: m.MerchantID,
			Name:       m.Name,
		}
	}
	return result
}

func merchantsFromProto(merchants []*pb.Merchant) []*domain.Merchant {
	result := make([]*domain.Merchant, len(merchants))
	for i, m := range merchants {
		result[i] = &domain.Merchant{
			MerchantID: m.MerchantId,
			Name:       m.Name,
		}
	}
	return result
}

func itemsToProto(items []*domain.Item) []*pb.Item {
	result := make([]*pb.Item, len(items))
	for i, item := range items {
		result[i] = &pb.Item{
			ItemId:     item.ItemID,
			Name:       item.Name,
			CategoryId: item.CategoryID,
			MerchantId: item.MerchantID,
			Normalized: item.Normalized,
			Aliases:    item.Aliases,
		}
	}
	return result
}

func itemsFromProto(items []*pb.Item) []*domain.Item {
	result := make([]*domain.Item, len(items))
	for i, item := range items {
		result[i] = &domain.Item{
			ItemID:     item.ItemId,
			Name:       item.Name,
			CategoryID: item.CategoryId,
			MerchantID: item.MerchantId,
			Normalized: item.Normalized,
			Aliases:    item.Aliases,
		}
	}
	return result
}

func receiptsToProto(receipts []*domain.Receipt) []*pb.Receipt {
	result := make([]*pb.Receipt, len(receipts))
	for i, r := range receipts {
		items := make([]*pb.ReceiptItem, len(r.Items))
		for j, item := range r.Items {
			items[j] = &pb.ReceiptItem{
				ItemId:         item.ItemID,
				Quantity:       item.Quantity,
				UnitPriceCents: item.UnitPriceCents,
			}
		}
		result[i] = &pb.Receipt{
			ReceiptId:  r.ReceiptID,
			MerchantId: r.MerchantID,
			OwnerId:    r.OwnerID,
			Date:       uint64(r.Date),
			PhotoUrl:   r.PhotoURL,
			Items:      items,
			TotalCents: r.TotalCents,
		}
	}
	return result
}

func receiptsFromProto(receipts []*pb.Receipt) []*domain.Receipt {
	result := make([]*domain.Receipt, len(receipts))
	for i, r := range receipts {
		items := make([]domain.ReceiptItem, len(r.Items))
		for j, item := range r.Items {
			items[j] = domain.ReceiptItem{
				ItemID:         item.ItemId,
				Quantity:       item.Quantity,
				UnitPriceCents: item.UnitPriceCents,
			}
		}
		result[i] = &domain.Receipt{
			ReceiptID:  r.ReceiptId,
			MerchantID: r.MerchantId,
			OwnerID:    r.OwnerId,
			Date:       int64(r.Date),
			PhotoURL:   r.PhotoUrl,
			Items:      items,
			TotalCents: r.TotalCents,
		}
	}
	return result
}

func proposalsToProto(proposals []*domain.Proposal) []*pb.Proposal {
	result := make([]*pb.Proposal, len(proposals))
	for i, p := range proposals {
		items := make([]*pb.ProposalItem, len(p.Items))
		for j, item := range p.Items {
			items[j] = &pb.ProposalItem{
				ParsedName:      item.ParsedName,
				Quantity:        item.Quantity,
				UnitPriceCents:  item.UnitPriceCents,
				MatchedItemId:   item.MatchedItemID,
				CategoryId:      item.CategoryID,
				IsNewCategory:   item.IsNewCategory,
				UserChoice:      item.UserChoice,
				OcrConfidence:   item.OcrConfidence,
				SourceBlockType: item.SourceBlockType,
				TotalPriceCents: item.TotalPriceCents,
			}
		}
		result[i] = &pb.Proposal{
			ProposalId:       p.ProposalID,
			OwnerId:          p.OwnerID,
			MerchantId:       p.MerchantID,
			Merchant:         p.Merchant,
			Date:             uint64(p.Date),
			PhotoUrl:         p.PhotoURL,
			Items:            items,
			TotalCents:       p.TotalCents,
			Status:           p.Status,
			OriginalHash:     p.OriginalHash,
			OcrMarkdown:      p.OcrMarkdown,
			OcrMinConfidence: p.OcrMinConfidence,
		}
	}
	return result
}

func proposalsFromProto(proposals []*pb.Proposal) []*domain.Proposal {
	result := make([]*domain.Proposal, len(proposals))
	for i, p := range proposals {
		items := make([]domain.ProposalItem, len(p.Items))
		for j, item := range p.Items {
			items[j] = domain.ProposalItem{
				ParsedName:      item.ParsedName,
				Quantity:        item.Quantity,
				UnitPriceCents:  item.UnitPriceCents,
				MatchedItemID:   item.MatchedItemId,
				CategoryID:      item.CategoryId,
				IsNewCategory:   item.IsNewCategory,
				UserChoice:      item.UserChoice,
				OcrConfidence:   item.OcrConfidence,
				SourceBlockType: item.SourceBlockType,
				TotalPriceCents: item.TotalPriceCents,
			}
		}
		result[i] = &domain.Proposal{
			ProposalID:       p.ProposalId,
			OwnerID:          p.OwnerId,
			MerchantID:       p.MerchantId,
			Merchant:         p.Merchant,
			Date:             int64(p.Date),
			PhotoURL:         p.PhotoUrl,
			Items:            items,
			TotalCents:       p.TotalCents,
			Status:           p.Status,
			OriginalHash:     p.OriginalHash,
			OcrMarkdown:      p.OcrMarkdown,
			OcrMinConfidence: p.OcrMinConfidence,
		}
	}
	return result
}

func botUsersToProto(botUsers []*BotUser) []*pb.BotUser {
	result := make([]*pb.BotUser, len(botUsers))
	for i, bu := range botUsers {
		result[i] = &pb.BotUser{
			ExternalId: bu.ExternalID,
			UserId:     bu.UserID,
		}
	}
	return result
}

func botUsersFromProto(botUsers []*pb.BotUser) []*BotUser {
	result := make([]*BotUser, len(botUsers))
	for i, bu := range botUsers {
		result[i] = &BotUser{
			ExternalID: bu.ExternalId,
			UserID:     bu.UserId,
		}
	}
	return result
}

func sessionsToProto(sessions []*Session) []*pb.Session {
	result := make([]*pb.Session, len(sessions))
	for i, s := range sessions {
		result[i] = &pb.Session{
			SessionId: s.SessionID,
			TokenHash: s.TokenHash,
			UserId:    s.UserID,
		}
	}
	return result
}

func sessionsFromProto(sessions []*pb.Session) []*Session {
	result := make([]*Session, len(sessions))
	for i, s := range sessions {
		result[i] = &Session{
			SessionID: s.SessionId,
			TokenHash: s.TokenHash,
			UserID:    s.UserId,
		}
	}
	return result
}
