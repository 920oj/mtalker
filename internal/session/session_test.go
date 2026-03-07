package session

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestSessionCloseRemovesTrackedTempFiles(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "voice.wav")
	if err := os.WriteFile(filePath, []byte("voice"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	session := New(CreateParams{})
	session.TrackTempFile(filePath)

	session.Close(context.Background())

	if session.TempFileCount() != 0 {
		t.Fatalf("TempFileCount() = %d, want 0", session.TempFileCount())
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed, stat err = %v", err)
	}
}

func TestSessionRemoveTempFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "voice.wav")
	if err := os.WriteFile(filePath, []byte("voice"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	session := New(CreateParams{})
	session.TrackTempFile(filePath)

	if err := session.RemoveTempFile(filePath); err != nil {
		t.Fatalf("RemoveTempFile() error = %v", err)
	}
	if session.TempFileCount() != 0 {
		t.Fatalf("TempFileCount() = %d, want 0", session.TempFileCount())
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed, stat err = %v", err)
	}
}

func TestSessionCloseCallsBeforeCloseHook(t *testing.T) {
	var calls atomic.Int32
	session := New(CreateParams{
		BeforeClose: func() {
			calls.Add(1)
		},
	})

	session.Close(context.Background())
	session.Close(context.Background())

	if calls.Load() != 1 {
		t.Fatalf("beforeClose calls = %d, want 1", calls.Load())
	}
}
