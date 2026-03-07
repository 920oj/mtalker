package config

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/disgoorg/snowflake/v2"
)

type Config struct {
	Token          string
	DICPath        string
	VoicePath      string
	OpenJTalkPath  string
	CommandGuildID *snowflake.ID
}

func Load() (Config, error) {
	cfg := Config{
		Token:     strings.TrimSpace(os.Getenv("DISGO_TOKEN")),
		DICPath:   strings.TrimSpace(os.Getenv("DICPATH")),
		VoicePath: strings.TrimSpace(os.Getenv("VOICEPATH")),
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

	commandGuildID, err := loadOptionalSnowflakeEnv("DISGO_COMMAND_GUILD_ID")
	if err != nil {
		return Config{}, err
	}
	cfg.CommandGuildID = commandGuildID

	return cfg, nil
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

func loadOptionalSnowflakeEnv(key string) (*snowflake.ID, error) {
	rawValue := strings.TrimSpace(os.Getenv(key))
	if rawValue == "" {
		return nil, nil
	}

	id, err := parseSnowflakeEnv(key, rawValue)
	if err != nil {
		return nil, err
	}
	return &id, nil
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
