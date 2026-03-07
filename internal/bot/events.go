package appbot

import (
	"context"
	"log/slog"
	"sync"

	appconfig "github.com/disgoorg/disgo/_examples/voice2/internal/config"
	"github.com/disgoorg/disgo/_examples/voice2/internal/session"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

type StartupVoicePlayer func(client *disgobot.Client, target appconfig.VoiceTarget, audioFile string) error

type Handler struct {
	startupVoiceTarget *appconfig.VoiceTarget
	startupAudioFile   string
	startupVoicePlayer StartupVoicePlayer
	sessions           *session.Manager
	readyOnce          sync.Once
}

func NewHandler(cfg appconfig.Config, startupVoicePlayer StartupVoicePlayer) *Handler {
	return &Handler{
		startupVoiceTarget: cfg.SampleVoiceTarget,
		startupAudioFile:   cfg.AudioFile,
		startupVoicePlayer: startupVoicePlayer,
		sessions:           session.NewManager(),
	}
}

func (h *Handler) Sessions() *session.Manager {
	return h.sessions
}

func (h *Handler) Close(ctx context.Context) {
	if h.sessions == nil {
		return
	}
	h.sessions.Close(ctx)
}

func (h *Handler) OnReady(event *events.Ready) {
	slog.Info("gateway ready", slog.String("session_id", event.SessionID))

	if h.startupVoiceTarget == nil {
		slog.Info("slash command handlers are ready")
		return
	}

	if h.startupVoicePlayer == nil {
		slog.Warn("startup voice playback requested but no player is configured")
		return
	}

	h.readyOnce.Do(func() {
		go func() {
			if err := h.startupVoicePlayer(event.Client(), *h.startupVoiceTarget, h.startupAudioFile); err != nil {
				slog.Error("startup voice playback stopped",
					slog.Any("err", err),
					slog.Uint64("guild_id", uint64(h.startupVoiceTarget.GuildID)),
					slog.Uint64("channel_id", uint64(h.startupVoiceTarget.ChannelID)),
				)
			}
		}()
	})
}

func (h *Handler) OnApplicationCommandInteractionCreate(event *events.ApplicationCommandInteractionCreate) {
	data := event.SlashCommandInteractionData()

	switch data.CommandName() {
	case ttsJoinCommandName:
		h.handleTTSJoin(event)
	case ttsDisconnectCommandName:
		h.handleTTSDisconnect(event)
	default:
		slog.Warn("received unsupported application command", slog.String("command_name", data.CommandName()))
	}
}

func (h *Handler) handleTTSJoin(event *events.ApplicationCommandInteractionCreate) {
	guildID := event.GuildID()
	if guildID == nil {
		h.respondEphemeral(event, "このコマンドはサーバー内でのみ使用できます。")
		return
	}
	if h.sessions != nil && h.sessions.Exists(*guildID) {
		slog.Info("rejected duplicate ttsjoin command",
			slog.Uint64("guild_id", uint64(*guildID)),
			slog.Uint64("actor_user_id", uint64(event.User().ID)),
		)
		h.respondEphemeral(event, "このサーバーでは既に読み上げセッションが起動しています。`/ttsdisconnect` を実行してから再度お試しください。")
		return
	}

	slog.Info("received ttsjoin command",
		slog.Uint64("guild_id", uint64(*guildID)),
		slog.Uint64("channel_id", uint64(event.Channel().ID())),
		slog.Uint64("actor_user_id", uint64(event.User().ID)),
	)

	h.respondEphemeral(
		event,
		"`/ttsjoin` を受け付けました。コマンド実行者が参加している VC への接続処理は Phase 3 で実装します。",
	)
}

func (h *Handler) handleTTSDisconnect(event *events.ApplicationCommandInteractionCreate) {
	guildID := event.GuildID()
	if guildID == nil {
		h.respondEphemeral(event, "このコマンドはサーバー内でのみ使用できます。")
		return
	}
	if h.sessions == nil || !h.sessions.Exists(*guildID) {
		h.respondEphemeral(event, "現在、このサーバーで接続中の読み上げセッションはありません。")
		return
	}

	slog.Info("received ttsdisconnect command",
		slog.Uint64("guild_id", uint64(*guildID)),
		slog.Uint64("channel_id", uint64(event.Channel().ID())),
		slog.Uint64("actor_user_id", uint64(event.User().ID)),
	)

	h.respondEphemeral(event, "`/ttsdisconnect` を受け付けました。切断処理は Phase 7 で実装します。")
}

func (h *Handler) respondEphemeral(event *events.ApplicationCommandInteractionCreate, content string) {
	if err := event.CreateMessage(discord.NewMessageCreate().WithContent(content).WithEphemeral(true)); err != nil {
		slog.Error("failed to create interaction response",
			slog.Any("err", err),
			slog.Uint64("interaction_id", uint64(event.ID())),
		)
	}
}
