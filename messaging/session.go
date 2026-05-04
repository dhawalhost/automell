package messaging

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Session tracks a single in-flight or completed LLM task triggered by a bot message.
type Session struct {
	ID           string // trigger message ID
	ChatID       string
	ParentID     string             // parent session ID for reply-branches; empty = root
	Cancel       context.CancelFunc // cancels the session's context
	StartedAt    time.Time
	RequestCount int    // total calls made within this session lifetime
	Status       string // "active" | "done" | "cancelled"
}

// SessionStore is a thread-safe in-memory store for bot sessions.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session // keyed by session ID (trigger message ID)
	byChat   map[string][]string // chatID → []sessionID
}

// NewSessionStore creates an empty store.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		byChat:   make(map[string][]string),
	}
}

// Create registers a new session and returns it with a derived context.
// The caller must ensure ctx is a long-lived context (e.g. bot run context).
func (s *SessionStore) Create(parentCtx context.Context, chatID, messageID, parentID string) (*Session, context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	sess := &Session{
		ID:        messageID,
		ChatID:    chatID,
		ParentID:  parentID,
		Cancel:    cancel,
		StartedAt: time.Now(),
		Status:    "active",
	}
	s.mu.Lock()
	s.sessions[messageID] = sess
	s.byChat[chatID] = append(s.byChat[chatID], messageID)
	s.mu.Unlock()
	return sess, ctx
}

// Get returns the session for the given ID, or nil if not found.
func (s *SessionStore) Get(sessionID string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[sessionID]
}

// MarkDone marks the session status as done.
func (s *SessionStore) MarkDone(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.Status = "done"
	}
}

// Cancel cancels a single session by ID. Returns true if it was active.
func (s *SessionStore) Cancel(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok || sess.Status != "active" {
		return false
	}
	sess.Cancel()
	sess.Status = "cancelled"
	return true
}

// CancelAll cancels all active sessions in a chat. Returns count of cancelled sessions.
func (s *SessionStore) CancelAll(chatID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, id := range s.byChat[chatID] {
		if sess, ok := s.sessions[id]; ok && sess.Status == "active" {
			sess.Cancel()
			sess.Status = "cancelled"
			count++
		}
	}
	return count
}

// Clear cancels all sessions in a chat and removes them from the store.
func (s *SessionStore) Clear(chatID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.byChat[chatID] {
		if sess, ok := s.sessions[id]; ok {
			if sess.Status == "active" {
				sess.Cancel()
			}
			delete(s.sessions, id)
		}
	}
	delete(s.byChat, chatID)
}

// Stats returns a human-readable status string for a chat.
func (s *SessionStore) Stats(chatID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.byChat[chatID]
	if len(ids) == 0 {
		return "No sessions recorded for this chat."
	}

	var active, done, cancelled int
	var lines []string
	for _, id := range ids {
		sess, ok := s.sessions[id]
		if !ok {
			continue
		}
		elapsed := time.Since(sess.StartedAt).Truncate(time.Second)
		switch sess.Status {
		case "active":
			active++
			lines = append(lines, fmt.Sprintf("  • [%s] active, started %s ago, %d request(s)", id, elapsed, sess.RequestCount))
		case "done":
			done++
		case "cancelled":
			cancelled++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Sessions: %d active / %d done / %d cancelled\n", active, done, cancelled))
	for _, l := range lines {
		sb.WriteString(l + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ResolveParentSession walks the parent chain from a given message ID (typically a
// reply-to message ID) to find the root session or any active ancestor.
// Returns the session if found, nil otherwise.
func (s *SessionStore) ResolveParentSession(replyToID string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[replyToID]
}
