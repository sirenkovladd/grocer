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
			// Include the web URL in the error so the user can find
			// the link-account flow. Previous version had no link
			// here, leaving users stuck.
			t.sendMessage(update.Message.Chat.ID,
				fmt.Sprintf("Unknown user. Link your account at the web app first: %s", t.webURL))
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

		// Fetch the proposal so we can include the item count and
		// total in the success message — gives the user a useful
		// one-line summary without opening the link.
		var itemCount int
		var totalCents int64
		if p, err := t.store.GetProposal(proposalID); err == nil {
			itemCount = len(p.Items)
			totalCents = p.TotalCents
		}
		total := float64(totalCents) / 100.0

		link := fmt.Sprintf("%s/#/proposals/%d", t.webURL, proposalID)
		t.sendMessage(update.Message.Chat.ID, fmt.Sprintf(
			"Receipt parsed: %d item%s, $%.2f total.\n[Review and approve →](%s)",
			itemCount, pluralS(itemCount), total, link,
		))
	}
}

// pluralS returns "s" if n != 1, used for "item" / "items" pluralization
// in bot messages.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (t *TelegramBot) sendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	t.bot.Send(msg)
}
