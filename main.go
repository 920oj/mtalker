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

const shutdownTimeout = 5 * time.Second

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
			gateway.IntentGuilds,
			gateway.IntentGuildVoiceStates,
			gateway.IntentGuildMessages,
			gateway.IntentMessageContent,
		)),
		bot.WithEventListenerFunc(handler.OnReady),
		bot.WithEventListenerFunc(handler.OnApplicationCommandInteractionCreate),
		bot.WithEventListenerFunc(handler.OnVoiceServerUpdate),
		bot.WithEventListenerFunc(handler.OnGuildVoiceStateUpdate),
		bot.WithEventListenerFunc(handler.OnMessageCreate),
		bot.WithVoiceManagerConfigOpts(
			voice.WithDaveSessionCreateFunc(golibdave.NewSession),
			voice.WithConnCreateFunc(appbot.NewVoiceConnCreateFunc(handler)),
		),
	)
	if err != nil {
		slog.Error("error creating client", slog.Any("err", err))
		return
	}
	shutdown := func(ctx context.Context) {
		if client == nil {
			return
		}
		handler.Close(ctx)
		client.Close(ctx)
		client = nil
	}

	if err := appbot.RegisterCommands(client, cfg.CommandGuildID); err != nil {
		slog.Error("error registering slash commands", slog.Any("err", err))
		shutdown(context.Background())
		return
	}

	if err = client.OpenGateway(context.TODO()); err != nil {
		slog.Error("error connecting to gateway", slog.Any("error", err))
		shutdown(context.Background())
		return
	}

	slog.Info("bot is now running. Press CTRL-C to exit.")
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(signals)

	firstSignal := <-signals
	slog.Info("shutdown signal received", slog.String("signal", firstSignal.String()))

	go func() {
		secondSignal := <-signals
		slog.Warn("received second shutdown signal, forcing exit", slog.String("signal", secondSignal.String()))
		os.Exit(1)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutdown(ctx)
}
