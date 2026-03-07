package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/disgoorg/disgo/voice"
)

const (
	DiscordSampleRate = 48000
	AudioChannels     = 2
	BytesPerSample    = 2
	MaxOpusPacketSize = 4000
	FrameDuration     = 20 * time.Millisecond

	speakingTimeout = 5 * time.Second
)

type OpusFrameStream interface {
	NextOpusFrame() ([]byte, error)
	Close() error
}

func SendOpusFrameStream(ctx context.Context, conn voice.Conn, stream OpusFrameStream) (err error) {
	if conn == nil {
		return errors.New("voice connection is nil")
	}
	if conn.UDP() == nil {
		return errors.New("voice udp connection is nil")
	}
	if stream == nil {
		return errors.New("opus frame stream is nil")
	}

	defer func() {
		if closeErr := stream.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	if err := setSpeaking(conn, voice.SpeakingFlagMicrophone); err != nil {
		return fmt.Errorf("set speaking on: %w", err)
	}
	defer func() {
		_ = setSpeaking(conn, voice.SpeakingFlagNone)
	}()

	ticker := time.NewTicker(FrameDuration)
	defer ticker.Stop()

	for immediate := true; ; {
		if !immediate {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
		immediate = false

		frame, frameErr := stream.NextOpusFrame()
		if frameErr != nil {
			if errors.Is(frameErr, io.EOF) {
				return nil
			}
			return frameErr
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if _, writeErr := conn.UDP().Write(frame); writeErr != nil {
			return writeErr
		}
	}
}

func setSpeaking(conn voice.Conn, flags voice.SpeakingFlags) error {
	ctx, cancel := context.WithTimeout(context.Background(), speakingTimeout)
	defer cancel()
	return conn.SetSpeaking(ctx, flags)
}
