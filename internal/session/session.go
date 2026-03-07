package session

import (
	"context"
	"errors"
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
	Content       string
	TextFilePath  string
	AudioFilePath string
}

type CreateParams struct {
	GuildID        snowflake.ID
	TextChannelID  snowflake.ID
	VoiceChannelID snowflake.ID
	Conn           voice.Conn
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

	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
	onClose   func()
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
		cancel := s.cancel
		conn := s.conn
		onClose := s.onClose
		s.mu.Unlock()

		if cancel != nil {
			cancel()
		}

		if ctx == nil {
			ctx = context.Background()
		}
		if conn != nil {
			conn.Close(ctx)
		}

		if onClose != nil {
			onClose()
		}
	})
}

func (s *Session) setOnClose(onClose func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onClose = onClose
}
