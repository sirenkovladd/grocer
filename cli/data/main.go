package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/store"
)

type JSONExport struct {
	Users      []*domain.User       `json:"users"`
	Categories []*domain.Category   `json:"categories"`
	Merchants  []*domain.Merchant   `json:"merchants"`
	Items      []*domain.Item       `json:"items"`
	Receipts   []*domain.Receipt    `json:"receipts"`
	Proposals  []*domain.Proposal   `json:"proposals"`
	BotUsers   []*store.BotUser     `json:"botUsers"`
	Sessions   []*store.Session     `json:"sessions"`
}

func main() {
	// Subcommands
	exportCmd := flag.NewFlagSet("export", flag.ExitOnError)
	exportFile := exportCmd.String("file", "snapshot.json", "Output JSON file")

	importCmd := flag.NewFlagSet("import", flag.ExitOnError)
	importFile := importCmd.String("file", "snapshot.json", "Input JSON file")

	listUsersCmd := flag.NewFlagSet("list-users", flag.ExitOnError)
	listSessionsCmd := flag.NewFlagSet("list-sessions", flag.ExitOnError)
	listBotUsersCmd := flag.NewFlagSet("list-bot-users", flag.ExitOnError)
	listCategoriesCmd := flag.NewFlagSet("list-categories", flag.ExitOnError)
	listMerchantsCmd := flag.NewFlagSet("list-merchants", flag.ExitOnError)
	listItemsCmd := flag.NewFlagSet("list-items", flag.ExitOnError)
	listReceiptsCmd := flag.NewFlagSet("list-receipts", flag.ExitOnError)
	listProposalsCmd := flag.NewFlagSet("list-proposals", flag.ExitOnError)

	deleteUserCmd := flag.NewFlagSet("delete-user", flag.ExitOnError)
	deleteUserID := deleteUserCmd.Uint64("id", 0, "User ID to delete")

	deleteSessionCmd := flag.NewFlagSet("delete-session", flag.ExitOnError)
	deleteSessionID := deleteSessionCmd.Uint64("id", 0, "Session ID to delete")

	deleteBotUserCmd := flag.NewFlagSet("delete-bot-user", flag.ExitOnError)
	deleteBotUserExtID := deleteBotUserCmd.String("external-id", "", "Bot user external ID to delete")

	deleteCategoryCmd := flag.NewFlagSet("delete-category", flag.ExitOnError)
	deleteCategoryID := deleteCategoryCmd.Uint64("id", 0, "Category ID to delete")

	deleteMerchantCmd := flag.NewFlagSet("delete-merchant", flag.ExitOnError)
	deleteMerchantID := deleteMerchantCmd.Uint64("id", 0, "Merchant ID to delete")

	deleteItemCmd := flag.NewFlagSet("delete-item", flag.ExitOnError)
	deleteItemID := deleteItemCmd.Uint64("id", 0, "Item ID to delete")

	deleteReceiptCmd := flag.NewFlagSet("delete-receipt", flag.ExitOnError)
	deleteReceiptID := deleteReceiptCmd.Uint64("id", 0, "Receipt ID to delete")

	deleteProposalCmd := flag.NewFlagSet("delete-proposal", flag.ExitOnError)
	deleteProposalID := deleteProposalCmd.Uint64("id", 0, "Proposal ID to delete")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx := context.Background()

	// Initialize store
	s, err := store.NewStore()
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}

	// Load snapshot
	gcsBucket := os.Getenv("GCS_BUCKET")
	gcsPrefix := os.Getenv("GCS_PREFIX")
	gcsCredsFile := os.Getenv("GCS_CREDENTIALS_FILE")

	if gcsBucket != "" {
		if gcsPrefix == "" {
			gcsPrefix = "snapshots/"
		}

		gcs, err := store.NewGCloudStorage(ctx, gcsCredsFile, gcsBucket, gcsPrefix)
		if err != nil {
			log.Fatalf("Failed to create GCloud storage: %v", err)
		}
		defer gcs.Close()

		s.SetSnapshotStorage(gcs)

		if err := s.LoadSnapshot(ctx); err != nil {
			log.Fatalf("Failed to load snapshot: %v", err)
		}
	}

	switch os.Args[1] {
	case "export":
		exportCmd.Parse(os.Args[2:])
		exportToJSON(s, *exportFile)

	case "import":
		importCmd.Parse(os.Args[2:])
		importFromJSON(s, *importFile)

	case "list-users":
		listUsersCmd.Parse(os.Args[2:])
		listUsers(s)

	case "list-sessions":
		listSessionsCmd.Parse(os.Args[2:])
		listSessions(s)

	case "list-bot-users":
		listBotUsersCmd.Parse(os.Args[2:])
		listBotUsers(s)

	case "list-categories":
		listCategoriesCmd.Parse(os.Args[2:])
		listCategories(s)

	case "list-merchants":
		listMerchantsCmd.Parse(os.Args[2:])
		listMerchants(s)

	case "list-items":
		listItemsCmd.Parse(os.Args[2:])
		listItems(s)

	case "list-receipts":
		listReceiptsCmd.Parse(os.Args[2:])
		listReceipts(s)

	case "list-proposals":
		listProposalsCmd.Parse(os.Args[2:])
		listProposals(s)

	case "delete-user":
		deleteUserCmd.Parse(os.Args[2:])
		if *deleteUserID == 0 {
			log.Fatal("--id is required")
		}
		deleteUser(s, *deleteUserID)

	case "delete-session":
		deleteSessionCmd.Parse(os.Args[2:])
		if *deleteSessionID == 0 {
			log.Fatal("--id is required")
		}
		deleteSession(s, *deleteSessionID)

	case "delete-bot-user":
		deleteBotUserCmd.Parse(os.Args[2:])
		if *deleteBotUserExtID == "" {
			log.Fatal("--external-id is required")
		}
		deleteBotUser(s, *deleteBotUserExtID)

	case "delete-category":
		deleteCategoryCmd.Parse(os.Args[2:])
		if *deleteCategoryID == 0 {
			log.Fatal("--id is required")
		}
		deleteCategory(s, *deleteCategoryID)

	case "delete-merchant":
		deleteMerchantCmd.Parse(os.Args[2:])
		if *deleteMerchantID == 0 {
			log.Fatal("--id is required")
		}
		deleteMerchant(s, *deleteMerchantID)

	case "delete-item":
		deleteItemCmd.Parse(os.Args[2:])
		if *deleteItemID == 0 {
			log.Fatal("--id is required")
		}
		deleteItem(s, *deleteItemID)

	case "delete-receipt":
		deleteReceiptCmd.Parse(os.Args[2:])
		if *deleteReceiptID == 0 {
			log.Fatal("--id is required")
		}
		deleteReceipt(s, *deleteReceiptID)

	case "delete-proposal":
		deleteProposalCmd.Parse(os.Args[2:])
		if *deleteProposalID == 0 {
			log.Fatal("--id is required")
		}
		deleteProposal(s, *deleteProposalID)

	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: data <command> [options]")
	fmt.Println("\nCommands:")
	fmt.Println("  export --file=<path>          Export snapshot to JSON")
	fmt.Println("  import --file=<path>          Import snapshot from JSON")
	fmt.Println("  list-users                    List all users")
	fmt.Println("  list-sessions                 List all sessions")
	fmt.Println("  list-bot-users                List all bot users")
	fmt.Println("  list-categories               List all categories")
	fmt.Println("  list-merchants                List all merchants")
	fmt.Println("  list-items                    List all items")
	fmt.Println("  list-receipts                 List all receipts")
	fmt.Println("  list-proposals                List all proposals")
	fmt.Println("  delete-user --id=<id>         Delete user by ID")
	fmt.Println("  delete-session --id=<id>      Delete session by ID")
	fmt.Println("  delete-bot-user --external-id=<id>  Delete bot user by external ID")
	fmt.Println("  delete-category --id=<id>     Delete category by ID")
	fmt.Println("  delete-merchant --id=<id>     Delete merchant by ID")
	fmt.Println("  delete-item --id=<id>         Delete item by ID")
	fmt.Println("  delete-receipt --id=<id>      Delete receipt by ID")
	fmt.Println("  delete-proposal --id=<id>     Delete proposal by ID")
}

func exportToJSON(s *store.Store, filename string) {
	ctx := context.Background()

	users, _ := s.ListUsers()
	categories, _ := s.ListCategories()
	merchants, _ := s.ListMerchants()
	items, _ := s.ListItems()
	receipts, _ := s.ListReceipts()
	proposals, _ := s.ListProposals()
	botUsers, _ := s.ListBotUsers()
	sessions, _ := s.ListSessions()

	export := JSONExport{
		Users:      users,
		Categories: categories,
		Merchants:  merchants,
		Items:      items,
		Receipts:   receipts,
		Proposals:  proposals,
		BotUsers:   botUsers,
		Sessions:   sessions,
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal JSON: %v", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		log.Fatalf("Failed to write file: %v", err)
	}

	fmt.Printf("Exported snapshot to %s\n", filename)
	fmt.Printf("  Users: %d\n", len(users))
	fmt.Printf("  Categories: %d\n", len(categories))
	fmt.Printf("  Merchants: %d\n", len(merchants))
	fmt.Printf("  Items: %d\n", len(items))
	fmt.Printf("  Receipts: %d\n", len(receipts))
	fmt.Printf("  Proposals: %d\n", len(proposals))
	fmt.Printf("  Bot Users: %d\n", len(botUsers))
	fmt.Printf("  Sessions: %d\n", len(sessions))

	// Save snapshot after export (to ensure consistency)
	_ = ctx
}

func importFromJSON(s *store.Store, filename string) {
	ctx := context.Background()

	data, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var export JSONExport
	if err := json.Unmarshal(data, &export); err != nil {
		log.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// Clear existing data (optional - could add a flag for this)
	fmt.Println("Importing data...")

	for _, user := range export.Users {
		if err := s.CreateUser(user); err != nil {
			log.Printf("Warning: Failed to import user %s: %v", user.Username, err)
		}
	}

	for _, cat := range export.Categories {
		if err := s.CreateCategory(cat); err != nil {
			log.Printf("Warning: Failed to import category %s: %v", cat.Name, err)
		}
	}

	for _, merchant := range export.Merchants {
		if err := s.CreateMerchant(merchant); err != nil {
			log.Printf("Warning: Failed to import merchant %s: %v", merchant.Name, err)
		}
	}

	for _, item := range export.Items {
		if err := s.CreateItem(item); err != nil {
			log.Printf("Warning: Failed to import item %s: %v", item.Name, err)
		}
	}

	for _, receipt := range export.Receipts {
		if err := s.CreateReceipt(receipt); err != nil {
			log.Printf("Warning: Failed to import receipt %d: %v", receipt.ReceiptID, err)
		}
	}

	for _, proposal := range export.Proposals {
		if err := s.CreateProposal(proposal); err != nil {
			log.Printf("Warning: Failed to import proposal %d: %v", proposal.ProposalID, err)
		}
	}

	for _, botUser := range export.BotUsers {
		if err := s.CreateBotUser(botUser); err != nil {
			log.Printf("Warning: Failed to import bot user %s: %v", botUser.ExternalID, err)
		}
	}

	for _, session := range export.Sessions {
		if err := s.CreateSession(session); err != nil {
			log.Printf("Warning: Failed to import session %d: %v", session.SessionID, err)
		}
	}

	// Save snapshot after import
	if err := s.SaveSnapshot(ctx); err != nil {
		log.Printf("Warning: Failed to save snapshot: %v", err)
	}

	fmt.Printf("Imported from %s\n", filename)
	fmt.Printf("  Users: %d\n", len(export.Users))
	fmt.Printf("  Categories: %d\n", len(export.Categories))
	fmt.Printf("  Merchants: %d\n", len(export.Merchants))
	fmt.Printf("  Items: %d\n", len(export.Items))
	fmt.Printf("  Receipts: %d\n", len(export.Receipts))
	fmt.Printf("  Proposals: %d\n", len(export.Proposals))
	fmt.Printf("  Bot Users: %d\n", len(export.BotUsers))
	fmt.Printf("  Sessions: %d\n", len(export.Sessions))
}

func listUsers(s *store.Store) {
	users, err := s.ListUsers()
	if err != nil {
		log.Fatalf("Failed to list users: %v", err)
	}

	fmt.Printf("Users (%d):\n", len(users))
	for _, user := range users {
		fmt.Printf("  ID: %d, Username: %s, Name: %s\n", user.UserID, user.Username, user.Name)
	}
}

func listSessions(s *store.Store) {
	sessions, err := s.ListSessions()
	if err != nil {
		log.Fatalf("Failed to list sessions: %v", err)
	}

	fmt.Printf("Sessions (%d):\n", len(sessions))
	for _, session := range sessions {
		fmt.Printf("  SessionID: %d, UserID: %d, TokenHash: %s...\n", 
			session.SessionID, session.UserID, session.TokenHash[:20])
	}
}

func listBotUsers(s *store.Store) {
	botUsers, err := s.ListBotUsers()
	if err != nil {
		log.Fatalf("Failed to list bot users: %v", err)
	}

	fmt.Printf("Bot Users (%d):\n", len(botUsers))
	for _, bu := range botUsers {
		fmt.Printf("  ExternalID: %s, UserID: %d\n", bu.ExternalID, bu.UserID)
	}
}

func listCategories(s *store.Store) {
	categories, err := s.ListCategories()
	if err != nil {
		log.Fatalf("Failed to list categories: %v", err)
	}

	fmt.Printf("Categories (%d):\n", len(categories))
	for _, cat := range categories {
		parent := "none"
		if cat.ParentID != nil {
			parent = strconv.FormatUint(*cat.ParentID, 10)
		}
		fmt.Printf("  ID: %d, Name: %s, Parent: %s\n", cat.CategoryID, cat.Name, parent)
	}
}

func listMerchants(s *store.Store) {
	merchants, err := s.ListMerchants()
	if err != nil {
		log.Fatalf("Failed to list merchants: %v", err)
	}

	fmt.Printf("Merchants (%d):\n", len(merchants))
	for _, merchant := range merchants {
		fmt.Printf("  ID: %d, Name: %s\n", merchant.MerchantID, merchant.Name)
	}
}

func listItems(s *store.Store) {
	items, err := s.ListItems()
	if err != nil {
		log.Fatalf("Failed to list items: %v", err)
	}

	fmt.Printf("Items (%d):\n", len(items))
	for _, item := range items {
		fmt.Printf("  ID: %d, Name: %s, CategoryID: %d, MerchantID: %d\n", 
			item.ItemID, item.Name, item.CategoryID, item.MerchantID)
	}
}

func listReceipts(s *store.Store) {
	receipts, err := s.ListReceipts()
	if err != nil {
		log.Fatalf("Failed to list receipts: %v", err)
	}

	fmt.Printf("Receipts (%d):\n", len(receipts))
	for _, receipt := range receipts {
		total := float64(receipt.TotalCents) / 100.0
		fmt.Printf("  ID: %d, MerchantID: %d, OwnerID: %d, Total: $%.2f, Items: %d\n", 
			receipt.ReceiptID, receipt.MerchantID, receipt.OwnerID, total, len(receipt.Items))
	}
}

func listProposals(s *store.Store) {
	proposals, err := s.ListProposals()
	if err != nil {
		log.Fatalf("Failed to list proposals: %v", err)
	}

	fmt.Printf("Proposals (%d):\n", len(proposals))
	for _, proposal := range proposals {
		total := float64(proposal.TotalCents) / 100.0
		fmt.Printf("  ID: %d, MerchantID: %d, OwnerID: %d, Status: %s, Total: $%.2f\n", 
			proposal.ProposalID, proposal.MerchantID, proposal.OwnerID, proposal.Status, total)
	}
}

func deleteUser(s *store.Store, id uint64) {
	user, err := s.GetUserByUserID(id)
	if err != nil {
		log.Fatalf("User not found: %v", err)
	}

	if err := s.DeleteUser(user.Username); err != nil {
		log.Fatalf("Failed to delete user: %v", err)
	}

	fmt.Printf("Deleted user: %s (ID: %d)\n", user.Username, id)
}

func deleteSession(s *store.Store, id uint64) {
	if err := s.DeleteSession(id); err != nil {
		log.Fatalf("Failed to delete session: %v", err)
	}

	fmt.Printf("Deleted session: %d\n", id)
}

func deleteBotUser(s *store.Store, externalID string) {
	if err := s.DeleteBotUser(externalID); err != nil {
		log.Fatalf("Failed to delete bot user: %v", err)
	}

	fmt.Printf("Deleted bot user: %s\n", externalID)
}

func deleteCategory(s *store.Store, id uint64) {
	if err := s.DeleteCategory(id); err != nil {
		log.Fatalf("Failed to delete category: %v", err)
	}

	fmt.Printf("Deleted category: %d\n", id)
}

func deleteMerchant(s *store.Store, id uint64) {
	if err := s.DeleteMerchant(id); err != nil {
		log.Fatalf("Failed to delete merchant: %v", err)
	}

	fmt.Printf("Deleted merchant: %d\n", id)
}

func deleteItem(s *store.Store, id uint64) {
	if err := s.DeleteItem(id); err != nil {
		log.Fatalf("Failed to delete item: %v", err)
	}

	fmt.Printf("Deleted item: %d\n", id)
}

func deleteReceipt(s *store.Store, id uint64) {
	if err := s.DeleteReceipt(id); err != nil {
		log.Fatalf("Failed to delete receipt: %v", err)
	}

	fmt.Printf("Deleted receipt: %d\n", id)
}

func deleteProposal(s *store.Store, id uint64) {
	if err := s.DeleteProposal(id); err != nil {
		log.Fatalf("Failed to delete proposal: %v", err)
	}

	fmt.Printf("Deleted proposal: %d\n", id)
}
