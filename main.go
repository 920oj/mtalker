package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/disgoorg/godave/golibdave"

	"github.com/disgoorg/disgo"
	appbot "github.com/disgoorg/disgo/_examples/voice2/internal/bot"
	appconfig "github.com/disgoorg/disgo/_examples/voice2/internal/config"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	slog.Info("starting up")
	slog.Info("disgo version", slog.String("version", disgo.Version))

	cfg, err := appconfig.Load()
	if err != nil {
		slog.Error("startup validation failed", slog.Any("err", err))
		return
	}

	startupAttrs := []any{
		slog.String("open_jtalk_path", cfg.OpenJTalkPath),
		slog.String("dic_path", cfg.DICPath),
		slog.String("voice_path", cfg.VoicePath),
	}
	if cfg.CommandGuildID != nil {
		startupAttrs = append(startupAttrs,
			slog.String("command_scope", "guild"),
			slog.Uint64("command_guild_id", uint64(*cfg.CommandGuildID)),
		)
	} else {
		startupAttrs = append(startupAttrs, slog.String("command_scope", "global"))
	}
	slog.Info("startup validation passed", startupAttrs...)

	handler := appbot.NewHandler(cfg)

	client, err := disgo.New(cfg.Token,
		bot.WithGatewayConfigOpts(gateway.WithIntents(
			gateway.IntentGuildVoiceStates,
			gateway.IntentGuildMessages,
			gateway.IntentMessageContent,
		)),
		bot.WithEventListenerFunc(handler.OnReady),
		bot.WithEventListenerFunc(handler.OnApplicationCommandInteractionCreate),
		bot.WithEventListenerFunc(handler.OnMessageCreate),
		bot.WithVoiceManagerConfigOpts(
			voice.WithDaveSessionCreateFunc(golibdave.NewSession),
		),
	)
	if err != nil {
		slog.Error("error creating client", slog.Any("err", err))
		return
	}

	if err := appbot.RegisterCommands(client, cfg.CommandGuildID); err != nil {
		slog.Error("error registering slash commands", slog.Any("err", err))
		return
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		handler.Close(ctx)
		client.Close(ctx)
	}()

	if err = client.OpenGateway(context.TODO()); err != nil {
		slog.Error("error connecting to gateway", slog.Any("error", err))
		return
	}

	slog.Info("bot is now running. Press CTRL-C to exit.")
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
}
