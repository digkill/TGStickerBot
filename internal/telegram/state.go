package telegram

import (
	"sync"

	"github.com/digkill/TGStickerBot/internal/models"
)

type SessionState int

const (
	StateIdle SessionState = iota
	StateAwaitingModel
	StateAwaitingPrompt
)

type Session struct {
	State         SessionState
	SelectedModel models.ModelType
	AspectRatio   string
	Resolution    string
	ReferenceURLs []string
}

type StateManager struct {
	mu       sync.RWMutex
	sessions map[int64]*Session
}

func NewStateManager() *StateManager {
	return &StateManager{
		sessions: make(map[int64]*Session),
	}
}

func (m *StateManager) Get(chatID int64) *Session {
	m.mu.RLock()
	session, ok := m.sessions[chatID]
	m.mu.RUnlock()
	if ok {
		return session
	}
	return &Session{
		State:         StateIdle,
		AspectRatio:   "1:1",
		Resolution:    "1K",
		ReferenceURLs: make([]string, 0),
	}
}

func (m *StateManager) Set(chatID int64, session *Session) {
	m.mu.Lock()
	m.sessions[chatID] = session
	m.mu.Unlock()
}

func (m *StateManager) Reset(chatID int64) {
	m.Set(chatID, &Session{
		State:         StateIdle,
		AspectRatio:   "1:1",
		Resolution:    "1K",
		ReferenceURLs: make([]string, 0),
	})
}

func (m *StateManager) ClearReferences(chatID int64) {
	m.mu.Lock()
	if session, ok := m.sessions[chatID]; ok {
		session.ReferenceURLs = make([]string, 0)
	}
	m.mu.Unlock()
}
