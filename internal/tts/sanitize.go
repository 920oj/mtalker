package tts

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxTextLength        = 140
	TruncatedPlaceholder = "以下省略"
	DefaultChannelName   = "channel"
)

var urlPattern = regexp.MustCompile(`https?://\S+`)

func NormalizeText(input string) string {
	normalized := strings.TrimSpace(input)
	if normalized == "" {
		return ""
	}

	normalized = urlPattern.ReplaceAllString(normalized, "URL")
	normalized = strings.NewReplacer("\r\n", "", "\n", "", "\r", "").Replace(normalized)
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return ""
	}

	if utf8.RuneCountInString(normalized) > MaxTextLength {
		return TruncatedPlaceholder
	}

	return normalized
}

func SanitizeFileNameComponent(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return DefaultChannelName
	}

	var builder strings.Builder
	lastWasSeparator := false

	for _, r := range trimmed {
		if isSafeFileNameRune(r) {
			builder.WriteRune(r)
			lastWasSeparator = false
			continue
		}

		if lastWasSeparator {
			continue
		}
		builder.WriteRune('_')
		lastWasSeparator = true
	}

	sanitized := strings.Trim(builder.String(), "._-")
	if sanitized == "" {
		return DefaultChannelName
	}
	return sanitized
}

func CreateTextFile(channelName string, content string, now time.Time) (string, error) {
	fileName := fmt.Sprintf("%s.%d.txt", SanitizeFileNameComponent(channelName), now.UnixNano())
	filePath := filepath.Join(os.TempDir(), fileName)

	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		return "", err
	}
	return filePath, nil
}

func isSafeFileNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-'
}
