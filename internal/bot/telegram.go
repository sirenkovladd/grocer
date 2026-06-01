package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramBot struct {
	token   string
	webURL  string
	store   *store.Store
	handler ReceiptHandler
	bot     *tgbotapi.BotAPI
}

func NewTelegramBot(token, webURL string, store *store.Store, handler ReceiptHandler) *TelegramBot {
	return &TelegramBot{
		token:   token,
		webURL:  webURL,
		store:   store,
		handler: handler,
	}
}

func (t *TelegramBot) Start(ctx context.Context) error {
	var err error
	t.bot, err = tgbotapi.NewBotAPI(t.token)
	if err != nil {
		return fmt.Errorf("NewBotAPI: %w", err)
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := t.bot.GetUpdatesChan(u)

	go func() {
		for {
			select {
			case <-ctx.Done():
				t.bot.StopReceivingUpdates()
				return
			case update := <-updates:
				t.handleUpdate(ctx, update)
			}
		}
	}()

	return nil
}

func (t *TelegramBot) Stop() error {
	t.bot.StopReceivingUpdates()
	return nil
}

func (t *TelegramBot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	// Handle photo messages
	if update.Message.Photo != nil {
		// Look up user by Telegram ID
		externalID := fmt.Sprintf("telegram:%d", update.Message.From.ID)
		botUser, err := t.store.GetBotUser(externalID)
		if err != nil {
			t.sendMessage(update.Message.Chat.ID, "Unknown user. Link your account at the web app first.")
			return
		}

		photo := update.Message.Photo[len(update.Message.Photo)-1]

		file, err := t.bot.GetFile(tgbotapi.FileConfig{FileID: photo.FileID})
		if err != nil {
			t.sendMessage(update.Message.Chat.ID, "Failed to get photo")
			return
		}

		resp, err := http.Get(file.Link(t.bot.Token))
		if err != nil {
			t.sendMessage(update.Message.Chat.ID, "Failed to download photo")
			return
		}
		defer resp.Body.Close()

		photoData, err := io.ReadAll(resp.Body)
		if err != nil {
			t.sendMessage(update.Message.Chat.ID, "Failed to read photo")
			return
		}

		senderID := strconv.FormatUint(botUser.UserID, 10)
		proposalID, err := t.handler.HandlePhoto(ctx, photoData, senderID)
		if err != nil {
			t.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Failed to parse receipt: %v", err))
			return
		}

		link := fmt.Sprintf("%s/#/proposals/%d", t.webURL, proposalID)
		t.sendMessage(update.Message.Chat.ID, fmt.Sprintf("Receipt parsed! [Review and approve →](%s)", link))
	}
}

func (t *TelegramBot) sendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	t.bot.Send(msg)
}
