package appbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	appconfig "github.com/disgoorg/disgo/_examples/voice2/internal/config"
	"github.com/disgoorg/disgo/_examples/voice2/internal/session"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

const voiceConnectTimeout = 10 * time.Second

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
	if err := event.DeferCreateMessage(true); err != nil {
		slog.Error("failed to defer ttsjoin interaction response",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(*guildID)),
			slog.Uint64("actor_user_id", uint64(event.User().ID)),
		)
		return
	}

	slog.Info("received ttsjoin command",
		slog.Uint64("guild_id", uint64(*guildID)),
		slog.Uint64("channel_id", uint64(event.Channel().ID())),
		slog.Uint64("actor_user_id", uint64(event.User().ID)),
	)

	voiceState, err := h.resolveUserVoiceState(event.Client(), *guildID, event.User().ID)
	if err != nil {
		slog.Error("failed to resolve command user voice state",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(*guildID)),
			slog.Uint64("actor_user_id", uint64(event.User().ID)),
		)
		h.updateDeferredResponse(event, "コマンド実行者の Voice State を取得できませんでした。少し待ってから再度お試しください。")
		return
	}
	if voiceState == nil || voiceState.ChannelID == nil {
		slog.Info("command user is not connected to a voice channel",
			slog.Uint64("guild_id", uint64(*guildID)),
			slog.Uint64("actor_user_id", uint64(event.User().ID)),
		)
		h.updateDeferredResponse(event, "ボイスチャンネルに参加してから `/ttsjoin` を実行してください。")
		return
	}

	voiceChannelID := *voiceState.ChannelID
	sess, err := h.openVoiceSession(event.Client(), *guildID, event.Channel().ID(), voiceChannelID)
	if err != nil {
		if errors.Is(err, session.ErrSessionAlreadyExists) {
			h.updateDeferredResponse(event, "このサーバーでは既に読み上げセッションが起動しています。`/ttsdisconnect` を実行してから再度お試しください。")
			return
		}

		slog.Error("failed to open tts voice session",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(*guildID)),
			slog.Uint64("voice_channel_id", uint64(voiceChannelID)),
			)
		h.updateDeferredResponse(event, "ボイスチャンネルへの接続に失敗しました。Bot に接続権限があるか確認して、再度お試しください。")
		return
	}

	go h.monitorVoiceSession(sess)

	h.updateDeferredResponse(
		event,
		fmt.Sprintf("ボイスチャンネル %s に接続しました。読み上げ対象テキストチャンネルは %s です。投稿の逐次読み上げは Phase 4 で実装します。", formatChannelMention(voiceChannelID), formatChannelMention(event.Channel().ID())),
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

	ctx, cancel := context.WithTimeout(context.Background(), voiceConnectTimeout)
	defer cancel()

	if err := h.sessions.Destroy(ctx, *guildID); err != nil {
		slog.Error("failed to destroy voice session",
			slog.Any("err", err),
			slog.Uint64("guild_id", uint64(*guildID)),
		)
		h.respondEphemeral(event, "読み上げセッションの切断に失敗しました。少し待ってから再度お試しください。")
		return
	}

	h.respondEphemeral(event, "読み上げセッションを切断しました。")
}

func (h *Handler) respondEphemeral(event *events.ApplicationCommandInteractionCreate, content string) {
	if err := event.CreateMessage(discord.NewMessageCreate().WithContent(content).WithEphemeral(true)); err != nil {
		slog.Error("failed to create interaction response",
			slog.Any("err", err),
			slog.Uint64("interaction_id", uint64(event.ID())),
		)
	}
}

func (h *Handler) updateDeferredResponse(event *events.ApplicationCommandInteractionCreate, content string) {
	if _, err := event.Client().Rest.UpdateInteractionResponse(
		event.ApplicationID(),
		event.Token(),
		discord.NewMessageUpdate().WithContent(content),
	); err != nil {
		slog.Error("failed to update deferred interaction response",
			slog.Any("err", err),
			slog.Uint64("interaction_id", uint64(event.ID())),
		)
	}
}

func (h *Handler) resolveUserVoiceState(client *disgobot.Client, guildID snowflake.ID, userID snowflake.ID) (*discord.VoiceState, error) {
	if client.Caches != nil {
		if voiceState, ok := client.Caches.VoiceState(guildID, userID); ok {
			return &voiceState, nil
		}
	}

	voiceState, err := client.Rest.GetUserVoiceState(guildID, userID)
	if err != nil {
		return nil, fmt.Errorf("get user voice state from rest: %w", err)
	}
	return voiceState, nil
}

func (h *Handler) openVoiceSession(client *disgobot.Client, guildID snowflake.ID, textChannelID snowflake.ID, voiceChannelID snowflake.ID) (*session.Session, error) {
	if h.sessions == nil {
		return nil, errors.New("session manager is not initialized")
	}

	conn := client.VoiceManager.CreateConn(guildID)
	sess, err := h.sessions.Create(session.CreateParams{
		GuildID:        guildID,
		TextChannelID:  textChannelID,
		VoiceChannelID: voiceChannelID,
		Conn:           conn,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), voiceConnectTimeout)
	defer cancel()
	if err := conn.Open(ctx, voiceChannelID, false, false); err != nil {
		destroyCtx, destroyCancel := context.WithTimeout(context.Background(), voiceConnectTimeout)
		defer destroyCancel()
		_ = h.sessions.Destroy(destroyCtx, guildID)
		return nil, fmt.Errorf("open voice connection: %w", err)
	}

	slog.Info("voice session started",
		slog.Uint64("guild_id", uint64(guildID)),
		slog.Uint64("text_channel_id", uint64(textChannelID)),
		slog.Uint64("voice_channel_id", uint64(voiceChannelID)),
	)

	return sess, nil
}

func (h *Handler) monitorVoiceSession(sess *session.Session) {
	conn := sess.Conn()
	if conn == nil || conn.UDP() == nil {
		return
	}

	for {
		if _, err := conn.UDP().ReadPacket(); err != nil {
			if sess.Closed() || sess.Context().Err() != nil {
				return
			}

			slog.Warn("voice session udp reader stopped",
				slog.Any("err", err),
				slog.Uint64("guild_id", uint64(sess.GuildID())),
				slog.Uint64("voice_channel_id", uint64(sess.VoiceChannelID())),
			)

			closeCtx, cancel := context.WithTimeout(context.Background(), voiceConnectTimeout)
			defer cancel()
			sess.Close(closeCtx)
			return
		}
	}
}

func formatChannelMention(channelID snowflake.ID) string {
	return fmt.Sprintf("<#%d>", channelID)
}
