package tts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeTextReplacesURLAndNewlines(t *testing.T) {
	input := "  hello\nhttps://example.com/test\r\nworld  "
	got := NormalizeText(input)
	want := "helloURLworld"
	if got != want {
		t.Fatalf("NormalizeText() = %q, want %q", got, want)
	}
}

func TestNormalizeTextTruncatesOverMaxLength(t *testing.T) {
	input := strings.Repeat("あ", MaxTextLength+1)
	got := NormalizeText(input)
	want := TruncatedPlaceholder
	if got != want {
		t.Fatalf("NormalizeText() = %q, want %q", got, want)
	}
}

func TestNormalizeTextReturnsEmptyWhenWhitespaceOnly(t *testing.T) {
	if got := NormalizeText(" \n\r\t "); got != "" {
		t.Fatalf("NormalizeText() = %q, want empty string", got)
	}
}

func TestSanitizeFileNameComponent(t *testing.T) {
	got := SanitizeFileNameComponent(" general/chat テスト!? ")
	want := "general_chat_テスト"
	if got != want {
		t.Fatalf("SanitizeFileNameComponent() = %q, want %q", got, want)
	}
}

func TestCreateTextFile(t *testing.T) {
	now := time.Unix(1700000000, 123)
	path, err := CreateTextFile("general/chat", "hello", now)
	if err != nil {
		t.Fatalf("CreateTextFile() error = %v", err)
	}
	defer os.Remove(path)

	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(os.TempDir()) {
		t.Fatalf("file dir = %q, want %q", filepath.Dir(path), os.TempDir())
	}
	if !strings.Contains(filepath.Base(path), "general_chat") {
		t.Fatalf("file name = %q, want sanitized channel name", filepath.Base(path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("file content = %q, want %q", string(data), "hello")
	}
}
