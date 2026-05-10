// Package telegram manages the Telegram Bot API connection using gotgbot/v2.
// It sets up the long-polling dispatcher, handles incoming message types,
// and provides methods to send replies back to the user.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
)

// MessageHandler processes an incoming text message.
type MessageHandler func(ctx context.Context, chatID int64, text string) error

// Bot wraps the gotgbot/v2 client with application-level routing and auth.
type Bot struct {
	api     *gotgbot.Bot
	allowed []int64
}

// Start enters a blocking long-polling loop that routes incoming updates to the
// provided MessageHandler. It returns when ctx is cancelled.
func (bot *Bot) Start(ctx context.Context, handler MessageHandler) error {
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(_ *gotgbot.Bot, _ *ext.Context, _ error) ext.DispatcherAction {
			return ext.DispatcherActionNoop
		},
		MaxRoutines: 1, // single-user concurrency for MVP
	})

	msgHandler := handlers.NewMessage(
		message.All,
		handlers.Response(func(b *gotgbot.Bot, extCtx *ext.Context) error {
			return bot.handleUpdate(ctx, extCtx, handler)
		}),
	)
	dispatcher.AddHandler(msgHandler)

	updater := ext.NewUpdater(dispatcher, nil)

	err := updater.StartPolling(bot.api, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout:        60,
			AllowedUpdates: []string{"message"},
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: 75 * time.Second,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("starting polling: %w", err)
	}

	<-ctx.Done()
	updater.Stop()
	return nil
}

// SendMessage delivers a plain text message to the specified chat.
func (bot *Bot) SendMessage(ctx context.Context, chatID int64, text string) error {
	_, err := bot.api.SendMessageWithContext(ctx, chatID, text, nil)
	if err != nil {
		return fmt.Errorf("sending message: %w", err)
	}
	return nil
}

// handleUpdate classifies an incoming update and routes text messages to the handler.
func (bot *Bot) handleUpdate(rootCtx context.Context, extCtx *ext.Context, handler MessageHandler) error {
	msg := extCtx.EffectiveMessage
	if msg == nil || msg.From == nil {
		return nil
	}
	if extCtx.EditedMessage != nil {
		return nil // ignore edits in MVP
	}
	if !bot.isAuthorized(msg.From.Id) {
		return nil // silently drop unauthorized
	}
	if bot.isTextMessage(msg) {
		return handler(rootCtx, msg.Chat.Id, msg.Text)
	}
	return bot.SendMessage(rootCtx, msg.Chat.Id, "Hilt only processes text and voice messages.")
}

// isTextMessage determines whether the message is plain text (no media attachments).
func (bot *Bot) isTextMessage(msg *gotgbot.Message) bool {
	if msg.Audio != nil {
		return false
	}
	if msg.Document != nil {
		return false
	}
	if len(msg.Photo) > 0 {
		return false
	}
	if msg.Sticker != nil {
		return false
	}
	if msg.Video != nil {
		return false
	}
	if msg.VideoNote != nil {
		return false
	}
	if msg.Voice != nil {
		return false
	}
	if msg.Contact != nil {
		return false
	}
	if msg.Location != nil {
		return false
	}
	return true
}

// isAuthorized returns true if the user is allowed to access the bot.
// An empty allowed list means unrestricted access.
func (bot *Bot) isAuthorized(userID int64) bool {
	if len(bot.allowed) == 0 {
		return true
	}
	for _, id := range bot.allowed {
		if id == userID {
			return true
		}
	}
	return false
}

// NewBot creates a new Telegram bot client and validates the token.
func NewBot(token string, allowed []int64) (*Bot, error) {
	if token == "" {
		return nil, errors.New("telegram bot token is empty")
	}

	api, err := gotgbot.NewBot(token, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client:             http.Client{Timeout: 75 * time.Second},
			DefaultRequestOpts: &gotgbot.RequestOpts{Timeout: 75 * time.Second},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	return &Bot{api: api, allowed: allowed}, nil
}
