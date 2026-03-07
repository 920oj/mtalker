package tts

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenJTalkSynthesizerSynthesize(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "open_jtalk")
	writeExecutable(t, scriptPath, `#!/bin/sh
out=""
dic=""
voice=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -x) dic="$2"; shift 2 ;;
    -m) voice="$2"; shift 2 ;;
    -ow) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -z "$out" ] || [ -z "$dic" ] || [ -z "$voice" ]; then
  echo "missing args" >&2
  exit 1
fi
cat > "$out"
`)

	textFilePath := filepath.Join(tempDir, "input.txt")
	if err := os.WriteFile(textFilePath, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	synthesizer := NewOpenJTalkSynthesizer(OpenJTalkConfig{
		CommandPath:    scriptPath,
		DictionaryPath: filepath.Join(tempDir, "dic"),
		VoicePath:      filepath.Join(tempDir, "voice.htsvoice"),
		TempDir:        tempDir,
	})

	result, err := synthesizer.Synthesize(textFilePath, time.Unix(1700000000, 456))
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}

	if !strings.HasPrefix(filepath.Base(result.AudioFilePath), "voice_") {
		t.Fatalf("AudioFilePath = %q, want voice_*.wav", result.AudioFilePath)
	}
	if filepath.Ext(result.AudioFilePath) != ".wav" {
		t.Fatalf("AudioFilePath ext = %q, want .wav", filepath.Ext(result.AudioFilePath))
	}

	data, err := os.ReadFile(result.AudioFilePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("generated content = %q, want %q", string(data), "hello world")
	}
}

func TestOpenJTalkSynthesizerSynthesizeReturnsStderrOnFailure(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "open_jtalk")
	writeExecutable(t, scriptPath, `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -ow) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
echo "synthesis failed" >&2
touch "$out"
exit 1
`)

	textFilePath := filepath.Join(tempDir, "input.txt")
	if err := os.WriteFile(textFilePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	synthesizer := NewOpenJTalkSynthesizer(OpenJTalkConfig{
		CommandPath:    scriptPath,
		DictionaryPath: filepath.Join(tempDir, "dic"),
		VoicePath:      filepath.Join(tempDir, "voice.htsvoice"),
		TempDir:        tempDir,
	})

	_, err := synthesizer.Synthesize(textFilePath, time.Unix(1700000000, 789))
	if err == nil {
		t.Fatal("Synthesize() error = nil, want failure")
	}

	var synthesisErr *SynthesisError
	if !errors.As(err, &synthesisErr) {
		t.Fatalf("error type = %T, want *SynthesisError", err)
	}
	if !strings.Contains(synthesisErr.Stderr, "synthesis failed") {
		t.Fatalf("stderr = %q, want message", synthesisErr.Stderr)
	}
	if _, statErr := os.Stat(synthesisErr.OutputFilePath); !os.IsNotExist(statErr) {
		t.Fatalf("output file should be removed on failure, statErr = %v", statErr)
	}
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}