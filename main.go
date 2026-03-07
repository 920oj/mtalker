package main

import "C"
import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/disgoorg/godave/golibdave"
	"github.com/hajimehoshi/go-mp3"
	"github.com/kazzmir/opus-go/opus"

	"github.com/disgoorg/disgo"
	appbot "github.com/disgoorg/disgo/_examples/voice2/internal/bot"
	appconfig "github.com/disgoorg/disgo/_examples/voice2/internal/config"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
)

const (
	discordSampleRate  = 48000
	audioChannels      = 2
	bytesPerSample     = 2
	decodedChunkFrames = 4096
	maxOpusPacketSize  = 4000
)

type mp3Stream struct {
	file       *os.File
	decoder    *mp3.Decoder
	encoder    *opus.Encoder
	sourceRate int
	step       float64
	sourcePos  float64
	baseIndex  int
	left       []int16
	right      []int16
	ended      bool
	readBuf    []byte
	opusBuf    []byte
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	slog.Info("starting up")
	slog.Info("disgo version", slog.String("version", disgo.Version))

	cfg, err := appconfig.Load()
	if err != nil {
		slog.Error("startup validation failed", slog.Any("err", err))
		return
	}

	startupAttrs := []any{
		slog.String("open_jtalk_path", cfg.OpenJTalkPath),
		slog.String("dic_path", cfg.DICPath),
		slog.String("voice_path", cfg.VoicePath),
		slog.Bool("startup_voice_playback_enabled", cfg.HasSampleVoiceTarget()),
	}
	if cfg.CommandGuildID != nil {
		startupAttrs = append(startupAttrs,
			slog.String("command_scope", "guild"),
			slog.Uint64("command_guild_id", uint64(*cfg.CommandGuildID)),
		)
	} else {
		startupAttrs = append(startupAttrs, slog.String("command_scope", "global"))
	}
	slog.Info("startup validation passed", startupAttrs...)

	handler := appbot.NewHandler(cfg, play)

	client, err := disgo.New(cfg.Token,
		bot.WithGatewayConfigOpts(gateway.WithIntents(
			gateway.IntentGuildVoiceStates,
			gateway.IntentGuildMessages,
			gateway.IntentMessageContent,
		)),
		bot.WithEventListenerFunc(handler.OnReady),
		bot.WithEventListenerFunc(handler.OnApplicationCommandInteractionCreate),
		bot.WithEventListenerFunc(handler.OnMessageCreate),
		bot.WithVoiceManagerConfigOpts(
			voice.WithDaveSessionCreateFunc(golibdave.NewSession),
		),
	)
	if err != nil {
		slog.Error("error creating client", slog.Any("err", err))
		return
	}

	if err := appbot.RegisterCommands(client, cfg.CommandGuildID); err != nil {
		slog.Error("error registering slash commands", slog.Any("err", err))
		return
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		handler.Close(ctx)
		client.Close(ctx)
	}()

	if err = client.OpenGateway(context.TODO()); err != nil {
		slog.Error("error connecting to gateway", slog.Any("error", err))
		return
	}

	slog.Info("bot is now running. Press CTRL-C to exit.")
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
}

func play(client *bot.Client, target appconfig.VoiceTarget, audioFile string) error {
	slog.Info("opening startup voice connection",
		slog.Uint64("guild_id", uint64(target.GuildID)),
		slog.Uint64("channel_id", uint64(target.ChannelID)),
		slog.String("audio_file", audioFile),
	)

	conn := client.VoiceManager.CreateConn(target.GuildID)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	if err := conn.Open(ctx, target.ChannelID, false, false); err != nil {
		return fmt.Errorf("open voice connection: %w", err)
	}
	slog.Info("startup voice connection established",
		slog.Uint64("guild_id", uint64(target.GuildID)),
		slog.Uint64("channel_id", uint64(target.ChannelID)),
	)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second*10)
		defer closeCancel()
		conn.Close(closeCtx)
		slog.Info("startup voice connection closed", slog.Uint64("guild_id", uint64(target.GuildID)))
	}()

	if err := conn.SetSpeaking(ctx, voice.SpeakingFlagMicrophone); err != nil {
		return fmt.Errorf("set speaking flag: %w", err)
	}
	go func() {
		for {
			if _, err := conn.UDP().ReadPacket(); err != nil {
				slog.Warn("voice udp reader stopped",
					slog.Any("err", err),
					slog.Uint64("guild_id", uint64(target.GuildID)),
				)
				return
			}
		}
	}()
	for {
		if err := writeMP3(conn.UDP(), audioFile); err != nil {
			return fmt.Errorf("write mp3: %w", err)
		}
	}
}

// writeMP3 decodes an MP3 file, converts it to 48kHz stereo PCM, encodes it as
// Opus and writes 20ms frames to the io.Writer.

func writeMP3(w io.Writer, audioFile string) error {
	stream, err := newMP3Stream(audioFile)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := stream.Close(); closeErr != nil {
			slog.Warn("error closing mp3 stream", slog.Any("err", closeErr))
		}
	}()

	ticker := time.NewTicker(time.Millisecond * 20)
	defer ticker.Stop()

	// Don't wait for the first tick, run immediately.
	for ; true; <-ticker.C {
		frame, err := stream.NextOpusFrame()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if _, err := w.Write(frame); err != nil {
			return err
		}
	}

	return nil
}

func newMP3Stream(path string) (*mp3Stream, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	decoder, err := mp3.NewDecoder(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	sourceRate := decoder.SampleRate()
	if sourceRate <= 0 {
		_ = file.Close()
		return nil, errors.New("invalid mp3 sample rate")
	}

	encoder, err := opus.NewEncoder(discordSampleRate, audioChannels, opus.ApplicationAudio)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	return &mp3Stream{
		file:       file,
		decoder:    decoder,
		encoder:    encoder,
		sourceRate: sourceRate,
		step:       float64(sourceRate) / float64(discordSampleRate),
		left:       make([]int16, 0, decodedChunkFrames),
		right:      make([]int16, 0, decodedChunkFrames),
		readBuf:    make([]byte, decodedChunkFrames*audioChannels*bytesPerSample),
		opusBuf:    make([]byte, maxOpusPacketSize),
	}, nil
}

func (s *mp3Stream) Close() error {
	var err error
	if s.encoder != nil {
		if closeErr := s.encoder.Close(); closeErr != nil {
			err = closeErr
		}
	}
	if s.file != nil {
		if closeErr := s.file.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func (s *mp3Stream) NextOpusFrame() ([]byte, error) {
	pcm := make([]int16, voice.OpusFrameSize*audioChannels)
	filled := 0

	for filled < voice.OpusFrameSize {
		left, right, ok, err := s.nextSample()
		if err != nil {
			return nil, err
		}
		if !ok {
			if filled == 0 {
				return nil, io.EOF
			}
			break
		}

		pcm[filled*audioChannels] = left
		pcm[filled*audioChannels+1] = right
		filled++
	}

	s.compactBuffers()

	n, err := s.encoder.Encode(pcm, voice.OpusFrameSize, s.opusBuf)
	if err != nil {
		return nil, err
	}
	return s.opusBuf[:n], nil
}

func (s *mp3Stream) nextSample() (int16, int16, bool, error) {
	if err := s.ensureSourceFor(s.sourcePos); err != nil {
		return 0, 0, false, err
	}

	index := int(math.Floor(s.sourcePos))
	relativeIndex := index - s.baseIndex
	if relativeIndex < 0 || relativeIndex >= len(s.left) {
		return 0, 0, false, nil
	}

	fraction := s.sourcePos - float64(index)
	left := s.left[relativeIndex]
	right := s.right[relativeIndex]
	nextLeft := left
	nextRight := right
	if relativeIndex+1 < len(s.left) {
		nextLeft = s.left[relativeIndex+1]
		nextRight = s.right[relativeIndex+1]
	}

	s.sourcePos += s.step
	return lerpInt16(left, nextLeft, fraction), lerpInt16(right, nextRight, fraction), true, nil
}

func (s *mp3Stream) ensureSourceFor(position float64) error {
	neededIndex := int(math.Floor(position))

	for !s.ended {
		if neededIndex-s.baseIndex < len(s.left) {
			return nil
		}

		if err := s.readDecodedChunk(); err != nil {
			if err == io.EOF {
				s.ended = true
				return nil
			}
			return err
		}
	}

	return nil
}

func (s *mp3Stream) readDecodedChunk() error {
	n, err := io.ReadFull(s.decoder, s.readBuf)
	if err != nil && err != io.EOF && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}

	n -= n % (audioChannels * bytesPerSample)
	if n == 0 {
		return io.EOF
	}

	for i := 0; i < n; i += audioChannels * bytesPerSample {
		left := int16(binary.LittleEndian.Uint16(s.readBuf[i : i+bytesPerSample]))
		right := int16(binary.LittleEndian.Uint16(s.readBuf[i+bytesPerSample : i+(audioChannels*bytesPerSample)]))
		s.left = append(s.left, left)
		s.right = append(s.right, right)
	}

	if errors.Is(err, io.ErrUnexpectedEOF) || err == io.EOF {
		s.ended = true
	}

	return nil
}

func (s *mp3Stream) compactBuffers() {
	keepFrom := int(math.Floor(s.sourcePos)) - s.baseIndex - 1
	if keepFrom <= 0 {
		return
	}
	if keepFrom > len(s.left) {
		keepFrom = len(s.left)
	}

	copy(s.left, s.left[keepFrom:])
	s.left = s.left[:len(s.left)-keepFrom]
	copy(s.right, s.right[keepFrom:])
	s.right = s.right[:len(s.right)-keepFrom]
	s.baseIndex += keepFrom
}

func lerpInt16(a int16, b int16, fraction float64) int16 {
	if fraction <= 0 {
		return a
	}
	if fraction >= 1 {
		return b
	}

	value := float64(a) + (float64(b)-float64(a))*fraction
	if value > math.MaxInt16 {
		value = math.MaxInt16
	}
	if value < math.MinInt16 {
		value = math.MinInt16
	}
	return int16(math.Round(value))
}
