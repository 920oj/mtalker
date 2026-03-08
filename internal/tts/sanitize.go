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

var urlPattern     = regexp.MustCompile(`https?://\S+`)
var mentionPattern = regexp.MustCompile(`<@!?\d+>`)

// Mention holds a Discord user ID and their display name for TTS replacement.
type Mention struct {
	ID          string
	DisplayName string
}

func NormalizeText(input string, mentions []Mention) string {
	normalized := strings.TrimSpace(input)
	if normalized == "" {
		return ""
	}

	normalized = urlPattern.ReplaceAllString(normalized, "URL")
	normalized = replaceMentions(normalized, mentions)
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

// replaceMentions replaces <@ID> and <@!ID> patterns with "あっとDisplayName"
// for known mentions. Unknown mention patterns are removed.
func replaceMentions(input string, mentions []Mention) string {
	if len(mentions) == 0 {
		return mentionPattern.ReplaceAllString(input, "")
	}

	lookup := make(map[string]string, len(mentions))
	for _, m := range mentions {
		lookup[m.ID] = m.DisplayName
	}

	return mentionPattern.ReplaceAllStringFunc(input, func(match string) string {
		// Extract the numeric ID from <@ID> or <@!ID>
		inner := strings.TrimPrefix(match, "<@")
		inner = strings.TrimSuffix(inner, ">")
		inner = strings.TrimPrefix(inner, "!")

		if name, ok := lookup[inner]; ok {
			return fmt.Sprintf("あっと%s", name)
		}
		return ""
	})
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
	fileNamePattern := fmt.Sprintf("%s.%d.*.txt", SanitizeFileNameComponent(channelName), now.UnixNano())
	file, err := os.CreateTemp(os.TempDir(), fileNamePattern)
	if err != nil {
		return "", err
	}

	filePath := filepath.Clean(file.Name())
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(filePath)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(filePath)
		return "", err
	}

	return filePath, nil
}

func isSafeFileNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-'
}
