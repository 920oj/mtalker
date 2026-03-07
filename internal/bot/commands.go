package appbot

import (
	"fmt"
	"log/slog"

	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

const (
	ttsJoinCommandName       = "ttsjoin"
	ttsDisconnectCommandName = "ttsdisconnect"
)

func Commands() []discord.ApplicationCommandCreate {
	return []discord.ApplicationCommandCreate{
		discord.SlashCommandCreate{
			Name:        ttsJoinCommandName,
			Description: "コマンド実行者が参加しているボイスチャンネルへ接続します",
		},
		discord.SlashCommandCreate{
			Name:        ttsDisconnectCommandName,
			Description: "現在の読み上げ用ボイス接続を切断します",
		},
	}
}

func RegisterCommands(client *disgobot.Client, guildID *snowflake.ID) error {
	commands := Commands()

	if guildID != nil {
		if _, err := client.Rest.SetGuildCommands(client.ApplicationID, *guildID, commands); err != nil {
			return fmt.Errorf("register guild commands: %w", err)
		}
		slog.Info("registered guild slash commands",
			slog.Uint64("guild_id", uint64(*guildID)),
			slog.Int("command_count", len(commands)),
		)
		return nil
	}

	if _, err := client.Rest.SetGlobalCommands(client.ApplicationID, commands); err != nil {
		return fmt.Errorf("register global commands: %w", err)
	}

	slog.Info("registered global slash commands", slog.Int("command_count", len(commands)))
	return nil
}
