// Package session manages per-launch session tokens with sliding expiry.
package session

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

type Session struct {
	ID        string
	Token     string
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
}

type Manager struct {
	mu          sync.RWMutex
	idleTimeout time.Duration
	sessions    map[string]*Session
}

func NewManager(idle time.Duration) *Manager {
	if idle <= 0 {
		idle = 12 * time.Hour
	}
	return &Manager{idleTimeout: idle, sessions: map[string]*Session{}}
}

func (m *Manager) Create() (*Session, error) {
	id, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	tok, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	s := &Session{
		ID: id, Token: tok,
		CreatedAt: now, LastSeen: now,
		ExpiresAt: now.Add(m.idleTimeout),
	}
	m.mu.Lock()
	m.sessions[tok] = s
	m.mu.Unlock()
	return s, nil
}

func (m *Manager) Validate(token string) (*Session, bool) {
	if token == "" {
		return nil, false
	}
	m.mu.RLock()
	var found *Session
	for t, s := range m.sessions {
		if subtle.ConstantTimeCompare([]byte(t), []byte(token)) == 1 {
			found = s
			break
		}
	}
	m.mu.RUnlock()
	if found == nil {
		return nil, false
	}
	now := time.Now()
	if now.After(found.ExpiresAt) {
		m.mu.Lock()
		delete(m.sessions, found.Token)
		m.mu.Unlock()
		return nil, false
	}
	m.mu.Lock()
	found.LastSeen = now
	found.ExpiresAt = now.Add(m.idleTimeout)
	m.mu.Unlock()
	return found, true
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
