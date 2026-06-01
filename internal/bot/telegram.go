package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramBot struct {
	token   string
	webURL  string
	handler ReceiptHandler
	bot     *tgbotapi.BotAPI
}

func NewTelegramBot(token, webURL string, handler ReceiptHandler) *TelegramBot {
	return &TelegramBot{
		token:   token,
		webURL:  webURL,
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

		senderID := strconv.FormatInt(update.Message.From.ID, 10)
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
