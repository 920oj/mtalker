package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

const DefaultQueueCapacity = 32

var (
	ErrSessionClosed = errors.New("session is closed")
	ErrQueueFull     = errors.New("session queue is full")
)

type PlaybackRequest struct {
	Content      string
	TextFilePath string
}

type CreateParams struct {
	GuildID        snowflake.ID
	TextChannelID  snowflake.ID
	VoiceChannelID snowflake.ID
	Conn           voice.Conn
	BeforeClose    func()
	QueueCapacity  int
}

type Session struct {
	guildID        snowflake.ID
	textChannelID  snowflake.ID
	voiceChannelID snowflake.ID
	conn           voice.Conn
	queue          chan PlaybackRequest
	ctx            context.Context
	cancel         context.CancelFunc

	sendMu       sync.Mutex
	mu           sync.RWMutex
	closed       bool
	idleSpeaking bool
	closeOnce    sync.Once
	beforeClose  func()
	onClose      func()
	tempFiles    map[string]struct{}
}

func New(params CreateParams) *Session {
	queueCapacity := params.QueueCapacity
	if queueCapacity <= 0 {
		queueCapacity = DefaultQueueCapacity
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Session{
		guildID:        params.GuildID,
		textChannelID:  params.TextChannelID,
		voiceChannelID: params.VoiceChannelID,
		conn:           params.Conn,
		queue:          make(chan PlaybackRequest, queueCapacity),
		ctx:            ctx,
		cancel:         cancel,
		beforeClose:    params.BeforeClose,
		tempFiles:      make(map[string]struct{}),
	}
}

func (s *Session) GuildID() snowflake.ID {
	return s.guildID
}

func (s *Session) TextChannelID() snowflake.ID {
	return s.textChannelID
}

func (s *Session) VoiceChannelID() snowflake.ID {
	return s.voiceChannelID
}

func (s *Session) Conn() voice.Conn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.conn
}

func (s *Session) Queue() <-chan PlaybackRequest {
	return s.queue
}

func (s *Session) Context() context.Context {
	return s.ctx
}

func (s *Session) QueueLen() int {
	return len(s.queue)
}

func (s *Session) QueueCap() int {
	return cap(s.queue)
}

func (s *Session) Closed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

func (s *Session) Enqueue(request PlaybackRequest) error {
	s.mu.RLock()
	closed := s.closed
	queue := s.queue
	ctx := s.ctx
	s.mu.RUnlock()

	if closed {
		return ErrSessionClosed
	}

	select {
	case <-ctx.Done():
		return ErrSessionClosed
	case queue <- request:
		return nil
	default:
		return ErrQueueFull
	}
}

func (s *Session) Close(ctx context.Context) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.idleSpeaking = false
		cancel := s.cancel
		conn := s.conn
		s.conn = nil
		beforeClose := s.beforeClose
		onClose := s.onClose
		tempFiles := make([]string, 0, len(s.tempFiles))
		for path := range s.tempFiles {
			tempFiles = append(tempFiles, path)
		}
		s.tempFiles = make(map[string]struct{})
		s.mu.Unlock()

		if cancel != nil {
			cancel()
		}

		if ctx == nil {
			ctx = context.Background()
		}
		if beforeClose != nil {
			beforeClose()
		}
		s.sendMu.Lock()
		if err := closeVoiceConn(ctx, conn); err != nil {
			slog.Warn("failed to close voice connection",
				slog.Any("err", err),
				slog.Uint64("guild_id", uint64(s.guildID)),
				slog.Uint64("voice_channel_id", uint64(s.voiceChannelID)),
			)
		}
		s.sendMu.Unlock()
		for _, path := range tempFiles {
			_ = removeTrackedFile(path)
		}

		if onClose != nil {
			onClose()
		}
	})
}

func (s *Session) WithConnExclusive(action func(conn voice.Conn) error) error {
	if action == nil {
		return nil
	}

	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	if s.ctx.Err() != nil {
		return ErrSessionClosed
	}

	conn := s.Conn()
	if conn == nil {
		return ErrSessionClosed
	}

	return action(conn)
}

func (s *Session) IdleSpeakingActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idleSpeaking
}

func (s *Session) SetIdleSpeakingActive(active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		s.idleSpeaking = false
		return
	}
	s.idleSpeaking = active
}

func closeVoiceConn(ctx context.Context, conn voice.Conn) (err error) {
	if conn == nil {
		return nil
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic while closing voice connection: %v", recovered)
		}
	}()

	conn.Close(ctx)
	return nil
}

func (s *Session) TrackTempFile(path string) {
	if path == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.tempFiles[path] = struct{}{}
}

func (s *Session) RemoveTempFile(path string) error {
	if path == "" {
		return nil
	}

	s.mu.Lock()
	delete(s.tempFiles, path)
	s.mu.Unlock()

	return removeTrackedFile(path)
}

func (s *Session) TempFileCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tempFiles)
}

func (s *Session) setOnClose(onClose func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onClose = onClose
}

func removeTrackedFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
