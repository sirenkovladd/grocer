package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/store"
	"github.com/bwmarrin/discordgo"
)

type DiscordBot struct {
	token   string
	webURL  string
	store   *store.Store
	handler ReceiptHandler
	session *discordgo.Session
}

func NewDiscordBot(token, webURL string, store *store.Store, handler ReceiptHandler) *DiscordBot {
	return &DiscordBot{
		token:   token,
		webURL:  webURL,
		store:   store,
		handler: handler,
	}
}

func (d *DiscordBot) Start(ctx context.Context) error {
	var err error
	d.session, err = discordgo.New("Bot " + d.token)
	if err != nil {
		return fmt.Errorf("discordgo.New: %w", err)
	}

	d.session.AddHandler(d.handleMessage)

	if err := d.session.Open(); err != nil {
		return fmt.Errorf("Open: %w", err)
	}

	return nil
}

func (d *DiscordBot) Stop() error {
	return d.session.Close()
}

func (d *DiscordBot) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Handle messages with attachments (photos)
	for _, att := range m.Attachments {
		if att.ContentType == "image/jpeg" || att.ContentType == "image/png" {
			// Look up user by Discord ID
			externalID := fmt.Sprintf("discord:%s", m.Author.ID)
			botUser, err := d.store.GetBotUser(externalID)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "Unknown user. Link your account at the web app first.")
				continue
			}

			resp, err := http.Get(att.URL)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "Failed to download photo")
				continue
			}
			defer resp.Body.Close()

			photoData, err := io.ReadAll(resp.Body)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "Failed to read photo")
				continue
			}

			senderID := strconv.FormatUint(botUser.UserID, 10)
			proposalID, err := d.handler.HandlePhoto(context.Background(), photoData, senderID)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Failed to parse receipt: %v", err))
				continue
			}

			link := fmt.Sprintf("%s/#/proposals/%d", d.webURL, proposalID)
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Receipt parsed! [Review and approve →](%s)", link))
		}
	}
}
