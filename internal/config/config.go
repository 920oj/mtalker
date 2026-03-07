package config

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/disgoorg/snowflake/v2"
)

const defaultAudioFile = "rinascita-short.mp3"

type Config struct {
	Token             string
	DICPath           string
	VoicePath         string
	OpenJTalkPath     string
	AudioFile         string
	SampleVoiceTarget *VoiceTarget
}

type VoiceTarget struct {
	GuildID   snowflake.ID
	ChannelID snowflake.ID
}

func Load() (Config, error) {
	cfg := Config{
		Token:     strings.TrimSpace(os.Getenv("DISGO_TOKEN")),
		DICPath:   strings.TrimSpace(os.Getenv("DICPATH")),
		VoicePath: strings.TrimSpace(os.Getenv("VOICEPATH")),
		AudioFile: envOrDefault("DISGO_AUDIO_FILE", defaultAudioFile),
	}

	if err := cfg.validateRequired(); err != nil {
		return Config{}, err
	}

	openJTalkPath, err := exec.LookPath("open_jtalk")
	if err != nil {
		return Config{}, fmt.Errorf("open_jtalk not found in PATH: %w", err)
	}
	cfg.OpenJTalkPath = openJTalkPath

	if err := validateExistingPath("DICPATH", cfg.DICPath); err != nil {
		return Config{}, err
	}
	if err := validateExistingPath("VOICEPATH", cfg.VoicePath); err != nil {
		return Config{}, err
	}

	sampleVoiceTarget, err := loadOptionalVoiceTarget()
	if err != nil {
		return Config{}, err
	}
	cfg.SampleVoiceTarget = sampleVoiceTarget

	if cfg.SampleVoiceTarget != nil {
		if err := validateExistingPath("DISGO_AUDIO_FILE", cfg.AudioFile); err != nil {
			return Config{}, err
		}
	}

	return cfg, nil
}

func (c Config) HasSampleVoiceTarget() bool {
	return c.SampleVoiceTarget != nil
}

func (c Config) validateRequired() error {
	missing := make([]string, 0, 3)
	if c.Token == "" {
		missing = append(missing, "DISGO_TOKEN")
	}
	if c.DICPath == "" {
		missing = append(missing, "DICPATH")
	}
	if c.VoicePath == "" {
		missing = append(missing, "VOICEPATH")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func loadOptionalVoiceTarget() (*VoiceTarget, error) {
	guildIDRaw := strings.TrimSpace(os.Getenv("DISGO_GUILD_ID"))
	channelIDRaw := strings.TrimSpace(os.Getenv("DISGO_CHANNEL_ID"))

	switch {
	case guildIDRaw == "" && channelIDRaw == "":
		return nil, nil
	case guildIDRaw == "" || channelIDRaw == "":
		return nil, fmt.Errorf("DISGO_GUILD_ID and DISGO_CHANNEL_ID must both be set to enable startup voice playback")
	}

	guildID, err := parseSnowflakeEnv("DISGO_GUILD_ID", guildIDRaw)
	if err != nil {
		return nil, err
	}
	channelID, err := parseSnowflakeEnv("DISGO_CHANNEL_ID", channelIDRaw)
	if err != nil {
		return nil, err
	}

	return &VoiceTarget{
		GuildID:   guildID,
		ChannelID: channelID,
	}, nil
}

func parseSnowflakeEnv(key string, value string) (snowflake.ID, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an unsigned integer: %w", key, err)
	}
	return snowflake.ID(id), nil
}

func validateExistingPath(key string, value string) error {
	if _, err := os.Stat(value); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s does not exist: %s", key, value)
		}
		return fmt.Errorf("%s is invalid: %w", key, err)
	}
	return nil
}

func envOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
