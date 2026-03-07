package tts

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Synthesizer interface {
	Synthesize(textFilePath string, now time.Time) (SynthesisResult, error)
}

type OpenJTalkConfig struct {
	CommandPath    string
	DictionaryPath string
	VoicePath      string
	TempDir        string
}

type OpenJTalkSynthesizer struct {
	commandPath    string
	dictionaryPath string
	voicePath      string
	tempDir        string
}

type SynthesisResult struct {
	AudioFilePath string
}

type SynthesisError struct {
	Err            error
	Stderr         string
	OutputFilePath string
}

func (e *SynthesisError) Error() string {
	if e == nil {
		return ""
	}
	if e.Stderr == "" {
		return fmt.Sprintf("open_jtalk synthesis failed: %v", e.Err)
	}
	return fmt.Sprintf("open_jtalk synthesis failed: %v: %s", e.Err, e.Stderr)
}

func (e *SynthesisError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewOpenJTalkSynthesizer(cfg OpenJTalkConfig) *OpenJTalkSynthesizer {
	tempDir := cfg.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}

	return &OpenJTalkSynthesizer{
		commandPath:    cfg.CommandPath,
		dictionaryPath: cfg.DictionaryPath,
		voicePath:      cfg.VoicePath,
		tempDir:        tempDir,
	}
}

func (s *OpenJTalkSynthesizer) Synthesize(textFilePath string, now time.Time) (SynthesisResult, error) {
	if textFilePath == "" {
		return SynthesisResult{}, fmt.Errorf("text file path is required")
	}

	inputFile, err := os.Open(textFilePath)
	if err != nil {
		return SynthesisResult{}, fmt.Errorf("open text file: %w", err)
	}
	defer inputFile.Close()

	outputFilePath := filepath.Join(s.tempDir, fmt.Sprintf("voice_%d.wav", now.UnixNano()))
	cmd := exec.Command(s.commandPath, "-x", s.dictionaryPath, "-m", s.voicePath, "-ow", outputFilePath)
	cmd.Stdin = inputFile

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		_ = removeFileIfExists(outputFilePath)
		return SynthesisResult{}, &SynthesisError{
			Err:            err,
			Stderr:         stderr.String(),
			OutputFilePath: outputFilePath,
		}
	}

	return SynthesisResult{AudioFilePath: outputFilePath}, nil
}

func removeFileIfExists(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}