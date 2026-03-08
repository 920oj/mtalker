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
	got := NormalizeText(input, nil)
	want := "helloURLworld"
	if got != want {
		t.Fatalf("NormalizeText() = %q, want %q", got, want)
	}
}

func TestNormalizeTextRemovesMentions(t *testing.T) {
	mentions := []Mention{
		{ID: "1234567890123", DisplayName: "おーじぇい"},
		{ID: "111111111111", DisplayName: "たろう"},
	}
	tests := []struct {
		name     string
		input    string
		mentions []Mention
		want     string
	}{
		{"display name", "<@1234567890123> こんにちは", mentions, "あっとおーじぇい こんにちは"},
		{"nickname mention", "<@!1234567890123> こんにちは", mentions, "あっとおーじぇい こんにちは"},
		{"mention in middle", "さっき <@1234567890123> が言ってた", mentions, "さっき あっとおーじぇい が言ってた"},
		{"mention at end", "よろしく <@1234567890123>", mentions, "よろしく あっとおーじぇい"},
		{"multiple mentions in middle", "さっき <@111111111111> と <@1234567890123> が話してた", mentions, "さっき あっとたろう と あっとおーじぇい が話してた"},
		{"multiple mentions", "<@111111111111> <@1234567890123> テスト", mentions, "あっとたろう あっとおーじぇい テスト"},
		{"unknown mention removed", "<@9999999999999> hello", mentions, "hello"},
		{"unknown mention in middle removed", "hello <@9999999999999> world", mentions, "hello  world"},
		{"nil mentions removes all", "<@1234567890123> hello", nil, "hello"},
		{"mention only with name", "<@1234567890123>", mentions, "あっとおーじぇい"},
		{"mention only unknown", "<@9999999999999>", mentions, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeText(tt.input, tt.mentions)
			if got != tt.want {
				t.Fatalf("NormalizeText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeTextTruncatesOverMaxLength(t *testing.T) {
	input := strings.Repeat("あ", MaxTextLength+1)
	got := NormalizeText(input, nil)
	want := TruncatedPlaceholder
	if got != want {
		t.Fatalf("NormalizeText() = %q, want %q", got, want)
	}
}

func TestNormalizeTextReturnsEmptyWhenWhitespaceOnly(t *testing.T) {
	if got := NormalizeText(" \n\r\t ", nil); got != "" {
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

func TestCreateTextFileCreatesUniquePaths(t *testing.T) {
	now := time.Unix(1700000000, 123)
	path1, err := CreateTextFile("general/chat", "hello", now)
	if err != nil {
		t.Fatalf("CreateTextFile(path1) error = %v", err)
	}
	defer os.Remove(path1)

	path2, err := CreateTextFile("general/chat", "world", now)
	if err != nil {
		t.Fatalf("CreateTextFile(path2) error = %v", err)
	}
	defer os.Remove(path2)

	if path1 == path2 {
		t.Fatalf("CreateTextFile() paths are identical: %q", path1)
	}

	if filepath.Base(path1) == filepath.Base(path2) {
		t.Fatalf("CreateTextFile() file names are identical: %q", filepath.Base(path1))
	}
	if !strings.Contains(filepath.Base(path1), "general_chat") || !strings.Contains(filepath.Base(path2), "general_chat") {
		t.Fatalf("CreateTextFile() file names should contain sanitized channel name: %q / %q", filepath.Base(path1), filepath.Base(path2))
	}
	if !strings.Contains(filepath.Base(path1), ".txt") || !strings.Contains(filepath.Base(path2), ".txt") {
		t.Fatalf("CreateTextFile() file names should end with txt pattern: %q / %q", filepath.Base(path1), filepath.Base(path2))
	}
	if data, err := os.ReadFile(path2); err != nil {
		t.Fatalf("ReadFile(path2) error = %v", err)
	} else if string(data) != "world" {
		t.Fatalf("path2 content = %q, want %q", string(data), "world")
	}
}
