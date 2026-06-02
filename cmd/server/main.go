package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"code.sirenko.ca/grocer/internal/api"
	"code.sirenko.ca/grocer/internal/bot"
	"code.sirenko.ca/grocer/internal/domain"
	"code.sirenko.ca/grocer/internal/events"
	"code.sirenko.ca/grocer/internal/llm"
	"code.sirenko.ca/grocer/internal/photo"
	"code.sirenko.ca/grocer/internal/receipt"
	"code.sirenko.ca/grocer/internal/store"
	"cloud.google.com/go/storage"
	"golang.org/x/crypto/argon2"
	"google.golang.org/api/option"
)

func main() {
	// Flags
	createUser := flag.Bool("create-user", false, "Create a new user")
	name := flag.String("name", "", "User's display name")
	username := flag.String("username", "", "Username")
	password := flag.String("password", "", "Password")
	linkBot := flag.Bool("link-bot", false, "Link a bot account")
	telegramID := flag.String("telegram", "", "Telegram user ID")
	discordID := flag.String("discord", "", "Discord user ID")
	flag.Parse()

	ctx := context.Background()

	// Initialize store
	s, err := store.NewStore()
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}

	// Initialize GCloud snapshot storage
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

		// Load snapshot from GCloud
		if err := s.LoadSnapshot(ctx); err != nil {
			log.Fatalf("Failed to load snapshot: %v", err)
		}
	}

	// Handle create-user flag
	if *createUser {
		if *name == "" || *username == "" || *password == "" {
			log.Fatal("All flags required: --name, --username, --password")
		}

		passwordHash, err := hashPassword(*password)
		if err != nil {
			log.Fatalf("Failed to hash password: %v", err)
		}

		user := &domain.User{
			UserID:       s.UserID.Gen(),
			Name:         *name,
			Username:     *username,
			PasswordHash: passwordHash,
		}

		if err := s.CreateUser(user); err != nil {
			log.Fatalf("Failed to create user: %v", err)
		}

		// Save snapshot after creating user
		if err := s.SaveSnapshot(ctx); err != nil {
			log.Printf("Warning: Failed to save snapshot: %v", err)
		}

		fmt.Printf("User %s created successfully\n", *username)
		return
	}

	// Handle link-bot flag
	if *linkBot {
		if *username == "" {
			log.Fatal("--username is required")
		}
		if *telegramID == "" && *discordID == "" {
			log.Fatal("Either --telegram or --discord is required")
		}

		user, err := s.GetUserByUsername(*username)
		if err != nil || user == nil {
			log.Fatalf("User %s not found", *username)
		}

		externalID := ""
		if *telegramID != "" {
			externalID = "telegram:" + *telegramID
		}
		if *discordID != "" {
			externalID = "discord:" + *discordID
		}

		botUser := &store.BotUser{
			ExternalID: externalID,
			UserID:     user.UserID,
		}

		if err := s.CreateBotUser(botUser); err != nil {
			log.Fatalf("Failed to link bot account: %v", err)
		}

		// Save snapshot after linking bot
		if err := s.SaveSnapshot(ctx); err != nil {
			log.Printf("Warning: Failed to save snapshot: %v", err)
		}

		fmt.Printf("Bot account linked: %s -> %s\n", externalID, *username)
		return
	}

	// Initialize LLM provider
	llmProvider := os.Getenv("LLM_PROVIDER")
	llmAPIKey := os.Getenv("LLM_API_KEY")
	llmModel := os.Getenv("LLM_MODEL")

	var provider llm.Provider
	switch llmProvider {
	case "kimi":
		provider = llm.NewKimiProvider(llmAPIKey, llmModel)
	case "qwen":
		provider = llm.NewQwenProvider(llmAPIKey, llmModel)
	default:
		log.Fatalf("Unknown LLM provider: %s", llmProvider)
	}

	// Initialize receipt parser
	parser := receipt.NewParser(s, provider)

	// Initialize event hub and wire into parser
	eventHub := events.NewHub()
	parser.SetEventHub(eventHub)

	// Initialize photo store
	var photoStore photo.Store
	var photoCache *photo.LocalCache

	if gcsBucket != "" {
		var opts []option.ClientOption
		if gcsCredsFile != "" {
			opts = append(opts, option.WithCredentialsFile(gcsCredsFile))
		}
		pClient, err := storage.NewClient(ctx, opts...)
		if err != nil {
			log.Printf("Warning: Failed to create photo storage client: %v", err)
		} else {
			photoStore = photo.NewGCloudStore(pClient, gcsBucket, "photos/")
		}
	}

	photoCacheDir := os.Getenv("PHOTO_CACHE_DIR")
	if photoCacheDir == "" {
		photoCacheDir = "./cache/photos"
	}
	photoCache = photo.NewLocalCache(photoCacheDir, 500)

	// Initialize router
	router := api.NewRouter(s, parser, photoStore, photoCache, eventHub)

	// Initialize bots
	telegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	discordToken := os.Getenv("DISCORD_BOT_TOKEN")
	botWebURL := os.Getenv("BOT_WEB_URL")

	var bots []bot.Bot
	if telegramToken != "" {
		telegramBot := bot.NewTelegramBot(telegramToken, botWebURL, s, parser)
		bots = append(bots, telegramBot)
	}
	if discordToken != "" {
		discordBot := bot.NewDiscordBot(discordToken, botWebURL, s, parser)
		bots = append(bots, discordBot)
	}

	// Start bots
	for _, b := range bots {
		if err := b.Start(ctx); err != nil {
			log.Printf("Failed to start bot: %v", err)
		}
	}

	// Start server
	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	// Apply middleware
	handler := api.RecoveryMiddleware(api.LoggingMiddleware(router))

	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Println("Shutting down...")

		// Save snapshot before shutdown
		if err := s.SaveSnapshot(ctx); err != nil {
			log.Printf("Failed to save snapshot: %v", err)
		}

		// Stop bots
		for _, b := range bots {
			if err := b.Stop(); err != nil {
				log.Printf("Failed to stop bot: %v", err)
			}
		}

		// Shutdown server
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Failed to shutdown server: %v", err)
		}
	}()

	log.Printf("Server starting on %s", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, 64*1024, 3, 2, b64Salt, b64Hash), nil
}
