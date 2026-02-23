package telegram

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/action"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

// PipelineRunner abstracts the daily pipeline for the bot.
type PipelineRunner interface {
	RunDailyPipeline(ctx context.Context, targetDate time.Time) error
}

// Bot handles Telegram bot interactions.
type Bot struct {
	client         *Client
	authorizedUser int64
	webhookSecret  string
	queries        *sqlcdb.Queries
	prefs          *preferences.Store
	nylasClient    action.NylasEventCreator
	pipeline       PipelineRunner
	log            zerolog.Logger
	cmdLimiter     *rate.Limiter
	lastRegenerate time.Time
	regenMu        sync.Mutex
	conversation   *ConversationState
	convMu         sync.Mutex
}

// NewBot creates a new Bot instance.
func NewBot(
	client *Client,
	authorizedUser int64,
	webhookSecret string,
	queries *sqlcdb.Queries,
	prefs *preferences.Store,
	nylasClient action.NylasEventCreator,
	pipeline PipelineRunner,
	log zerolog.Logger,
) *Bot {
	return &Bot{
		client:         client,
		authorizedUser: authorizedUser,
		webhookSecret:  webhookSecret,
		queries:        queries,
		prefs:          prefs,
		nylasClient:    nylasClient,
		pipeline:       pipeline,
		log:            log.With().Str("component", "telegram_bot").Logger(),
		cmdLimiter:     rate.NewLimiter(rate.Every(6*time.Second), 10), // 10/min
	}
}

// handleUpdate dispatches an incoming update to the appropriate handler.
func (b *Bot) handleUpdate(parentCtx context.Context, u Update) {
	// Use a fresh context with timeout for async handling.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if u.CallbackQuery != nil {
		b.handleCallbackQuery(ctx, u.CallbackQuery)
		return
	}

	if u.Message != nil {
		b.handleMessage(ctx, u.Message)
	}
}

// handleMessage processes incoming text messages, dispatching to commands
// or the active conversation flow.
func (b *Bot) handleMessage(ctx context.Context, msg *Message) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// Extract command from text.
	cmd, args := parseCommand(text)

	// Any /command cancels active conversation.
	if cmd != "" {
		b.convMu.Lock()
		if b.conversation != nil {
			b.conversation = nil
		}
		b.convMu.Unlock()
	}

	// Check rate limit for commands.
	if cmd != "" && !b.cmdLimiter.Allow() {
		b.sendText(ctx, msg.Chat.ID, "Slow down\\! Rate limit: 10 commands per minute\\.")
		return
	}

	switch cmd {
	case "/start":
		b.cmdStart(ctx, msg)
	case "/today":
		b.cmdToday(ctx, msg)
	case "/tomorrow":
		b.cmdTomorrow(ctx, msg)
	case "/regenerate":
		b.cmdRegenerate(ctx, msg)
	case "/gaps":
		b.cmdGaps(ctx, msg)
	case "/goals":
		b.cmdGoals(ctx, msg)
	case "/addgoal":
		b.cmdAddGoal(ctx, msg)
	case "/preferences":
		b.cmdPreferences(ctx, msg)
	case "/block":
		b.cmdBlock(ctx, msg, args)
	case "/unblock":
		b.cmdUnblock(ctx, msg, args)
	case "/status":
		b.cmdStatus(ctx, msg)
	case "/help":
		b.cmdHelp(ctx, msg)
	default:
		// Check for active conversation.
		b.convMu.Lock()
		conv := b.conversation
		b.convMu.Unlock()

		if conv != nil {
			b.handleConversationText(ctx, msg, text)
		}
		// Ignore unrecognized non-command text.
	}
}

// parseCommand extracts the /command and remaining args from a message.
func parseCommand(text string) (cmd, args string) {
	if !strings.HasPrefix(text, "/") {
		return "", text
	}

	// Handle /command@botname format.
	parts := strings.SplitN(text, " ", 2)
	cmd = strings.SplitN(parts[0], "@", 2)[0]
	cmd = strings.ToLower(cmd)

	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return cmd, args
}

// sendText is a convenience wrapper that sends a MarkdownV2 message.
func (b *Bot) sendText(ctx context.Context, chatID int64, text string) {
	if _, err := b.client.SendMessage(ctx, chatID, text, &SendOpts{ParseMode: "MarkdownV2"}); err != nil {
		b.log.Error().Err(err).Int64("chat_id", chatID).Msg("failed to send message")
	}
}

// sendPlainText sends a message without any parse mode.
func (b *Bot) sendPlainText(ctx context.Context, chatID int64, text string) {
	if _, err := b.client.SendMessage(ctx, chatID, text, nil); err != nil {
		b.log.Error().Err(err).Int64("chat_id", chatID).Msg("failed to send message")
	}
}

// sendWithKeyboard sends a MarkdownV2 message with an inline keyboard.
func (b *Bot) sendWithKeyboard(ctx context.Context, chatID int64, text string, kb *InlineKeyboard) (int, error) {
	return b.client.SendMessage(ctx, chatID, text, &SendOpts{
		ParseMode:   "MarkdownV2",
		ReplyMarkup: kb,
	})
}
