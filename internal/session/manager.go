package session

import (
	"context"
	"errors"
	"sync"

	"github.com/disgoorg/snowflake/v2"
)

var (
	ErrSessionAlreadyExists = errors.New("session already exists for guild")
	ErrSessionNotFound      = errors.New("session not found")
)

type Manager struct {
	mu       sync.RWMutex
	sessions map[snowflake.ID]*Session
}

func NewManager() *Manager {
	return &Manager{
		sessions: make(map[snowflake.ID]*Session),
	}
}

func (m *Manager) Create(params CreateParams) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[params.GuildID]; exists {
		return nil, ErrSessionAlreadyExists
	}

	session := New(params)
	session.setOnClose(func() {
		m.remove(params.GuildID, session)
	})
	m.sessions[params.GuildID] = session

	return session, nil
}

func (m *Manager) Get(guildID snowflake.ID) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[guildID]
	return session, ok
}

func (m *Manager) Exists(guildID snowflake.ID) bool {
	_, ok := m.Get(guildID)
	return ok
}

func (m *Manager) Destroy(ctx context.Context, guildID snowflake.ID) error {
	session, ok := m.Get(guildID)
	if !ok {
		return ErrSessionNotFound
	}

	session.Close(ctx)
	return nil
}

func (m *Manager) Detach(guildID snowflake.ID) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[guildID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	delete(m.sessions, guildID)
	return session, nil
}

func (m *Manager) Close(ctx context.Context) {
	for _, session := range m.Snapshot() {
		session.Close(ctx)
	}
}

func (m *Manager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *Manager) Snapshot() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

func (m *Manager) remove(guildID snowflake.ID, session *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.sessions[guildID]
	if !ok || current != session {
		return
	}

	delete(m.sessions, guildID)
}
