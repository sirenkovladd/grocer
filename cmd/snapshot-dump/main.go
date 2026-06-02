package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/env"
	"code.sirenko.ca/grocer/internal/store"
)

func main() {
	env.LoadDotEnv(".env")

	bucket := os.Getenv("GCS_BUCKET")
	prefix := os.Getenv("GCS_PREFIX")
	if prefix == "" {
		prefix = "snapshots/"
	}
	credsFile := os.Getenv("GCS_CREDENTIALS_FILE")

	if bucket == "" || credsFile == "" {
		log.Fatal("GCS_BUCKET and GCS_CREDENTIALS_FILE required in .env or environment")
	}
	if credsFile == "" {
		log.Fatal("GCS_CREDENTIALS_FILE env var required (set in .env or environment)")
	}

	ctx := context.Background()
	gcs, err := store.NewGCloudStorage(ctx, credsFile, bucket, prefix)
	if err != nil {
		log.Fatalf("Failed to connect to GCS: %v", err)
	}
	defer gcs.Close()

	data, err := gcs.Pull(ctx)
	if err != nil {
		log.Fatalf("Failed to pull snapshot: %v", err)
	}
	if data == nil {
		log.Fatal("No snapshot found")
	}

	snapshot, err := store.DeserializeSnapshot(data)
	if err != nil {
		log.Fatalf("Failed to deserialize: %v", err)
	}

	output := map[string]interface{}{
		"users":      formatUsers(snapshot.Users),
		"categories": formatCategories(snapshot.Categories),
		"merchants":  formatMerchants(snapshot.Merchants),
		"items":      formatItems(snapshot.Items),
		"receipts":   formatReceipts(snapshot.Receipts, snapshot),
		"proposals":  formatProposals(snapshot.Proposals, snapshot),
		"botUsers":   snapshot.BotUsers,
		"sessions":   len(snapshot.Sessions),
	}

	out, _ := json.MarshalIndent(output, "", "  ")
	fmt.Println(string(out))
}

type userView struct {
	ID       uint64 `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

func formatUsers(users []*domain.User) []userView {
	result := make([]userView, len(users))
	for i, u := range users {
		result[i] = userView{ID: u.UserID, Name: u.Name, Username: u.Username}
	}
	return result
}

type categoryView struct {
	ID       uint64  `json:"id"`
	Name     string  `json:"name"`
	ParentID *uint64 `json:"parentId,omitempty"`
}

func formatCategories(cats []*domain.Category) []categoryView {
	result := make([]categoryView, len(cats))
	for i, c := range cats {
		result[i] = categoryView{ID: c.CategoryID, Name: c.Name, ParentID: c.ParentID}
	}
	return result
}

type merchantView struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

func formatMerchants(merchants []*domain.Merchant) []merchantView {
	result := make([]merchantView, len(merchants))
	for i, m := range merchants {
		result[i] = merchantView{ID: m.MerchantID, Name: m.Name}
	}
	return result
}

type itemView struct {
	ID         uint64   `json:"id"`
	Name       string   `json:"name"`
	CategoryID uint64   `json:"categoryId"`
	Category   string   `json:"category,omitempty"`
	MerchantID uint64   `json:"merchantId"`
	Merchant   string   `json:"merchant,omitempty"`
	Normalized string   `json:"normalized"`
	Aliases    []string `json:"aliases,omitempty"`
}

func formatItems(items []*domain.Item) []itemView {
	result := make([]itemView, len(items))
	for i, it := range items {
		result[i] = itemView{
			ID:         it.ItemID,
			Name:       it.Name,
			CategoryID: it.CategoryID,
			MerchantID: it.MerchantID,
			Normalized: it.Normalized,
			Aliases:    it.Aliases,
		}
	}
	return result
}

type receiptItemView struct {
	ItemID       uint64 `json:"itemId"`
	ItemName     string `json:"itemName,omitempty"`
	Quantity     uint32 `json:"quantity"`
	UnitPrice    string `json:"unitPrice"`
	TotalPrice   string `json:"totalPrice"`
}

type receiptView struct {
	ID        uint64            `json:"id"`
	Merchant  string            `json:"merchant"`
	OwnerID   uint64            `json:"ownerId"`
	Date      string            `json:"date"`
	PhotoURL  string            `json:"photoUrl,omitempty"`
	Items     []receiptItemView `json:"items"`
	Total     string            `json:"total"`
}

func formatReceipts(receipts []*domain.Receipt, snap *store.SnapshotData) []receiptView {
	merchantNames := make(map[uint64]string)
	for _, m := range snap.Merchants {
		merchantNames[m.MerchantID] = m.Name
	}
	itemNames := make(map[uint64]string)
	for _, it := range snap.Items {
		itemNames[it.ItemID] = it.Name
	}

	result := make([]receiptView, len(receipts))
	for i, r := range receipts {
		items := make([]receiptItemView, len(r.Items))
		for j, it := range r.Items {
			items[j] = receiptItemView{
				ItemID:     it.ItemID,
				ItemName:   itemNames[it.ItemID],
				Quantity:   it.Quantity,
				UnitPrice:  formatCents(it.UnitPriceCents),
				TotalPrice: formatCents(int64(it.Quantity) * it.UnitPriceCents),
			}
		}
		result[i] = receiptView{
			ID:       r.ReceiptID,
			Merchant: merchantNames[r.MerchantID],
			OwnerID:  r.OwnerID,
			Date:     time.Unix(r.Date, 0).Format("2006-01-02"),
			PhotoURL: r.PhotoURL,
			Items:    items,
			Total:    formatCents(r.TotalCents),
		}
	}
	return result
}

type proposalItemView struct {
	ParsedName string  `json:"parsedName"`
	Quantity   uint32  `json:"quantity"`
	UnitPrice  string  `json:"unitPrice"`
	TotalPrice string  `json:"totalPrice"`
	Confidence float64 `json:"confidence"`
	Matched    string  `json:"matched,omitempty"`
	MatchedID  uint64  `json:"matchedItemId,omitempty"`
}

type proposalView struct {
	ID       uint64             `json:"id"`
	Merchant string             `json:"merchant"`
	OwnerID  uint64             `json:"ownerId"`
	Date     string             `json:"date"`
	PhotoURL string             `json:"photoUrl,omitempty"`
	Items    []proposalItemView `json:"items"`
	Total    string             `json:"total"`
	Status   string             `json:"status"`
}

func formatProposals(proposals []*domain.Proposal, snap *store.SnapshotData) []proposalView {
	itemNames := make(map[uint64]string)
	for _, it := range snap.Items {
		itemNames[it.ItemID] = it.Name
	}

	result := make([]proposalView, len(proposals))
	for i, p := range proposals {
		items := make([]proposalItemView, len(p.Items))
		for j, it := range p.Items {
			piv := proposalItemView{
				ParsedName: it.ParsedName,
				Quantity:   it.Quantity,
				UnitPrice:  formatCents(it.UnitPriceCents),
				TotalPrice: formatCents(int64(it.Quantity) * it.UnitPriceCents),
				Confidence: it.Confidence,
				MatchedID:  it.MatchedItemID,
			}
			if it.MatchedItemID != 0 {
				piv.Matched = itemNames[it.MatchedItemID]
			}
			items[j] = piv
		}
		result[i] = proposalView{
			ID:       p.ProposalID,
			Merchant: p.Merchant,
			OwnerID:  p.OwnerID,
			Date:     formatDate(p.Date),
			PhotoURL: p.PhotoURL,
			Items:    items,
			Total:    formatCents(p.TotalCents),
			Status:   p.Status,
		}
	}
	return result
}

func formatCents(cents int64) string {
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

func formatDate(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02")
}
