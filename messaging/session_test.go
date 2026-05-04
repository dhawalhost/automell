package messaging

import (
	"context"
	"testing"
	"time"
)

func TestSessionCreateAndGet(t *testing.T) {
	store := NewSessionStore()
	sess, _ := store.Create(context.Background(), "chat1", "msg1", "")
	if sess == nil {
		t.Fatal("expected session, got nil")
	}
	if sess.Status != "active" {
		t.Errorf("status = %s, want active", sess.Status)
	}
	got := store.Get("msg1")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.ID != "msg1" {
		t.Errorf("ID = %s, want msg1", got.ID)
	}
}

func TestSessionCancel(t *testing.T) {
	store := NewSessionStore()
	_, ctx := store.Create(context.Background(), "chat1", "msg1", "")
	ok := store.Cancel("msg1")
	if !ok {
		t.Error("Cancel returned false, want true")
	}
	select {
	case <-ctx.Done():
		// good
	case <-time.After(100 * time.Millisecond):
		t.Error("context was not cancelled after store.Cancel")
	}
	// Cancelling again should return false (already cancelled)
	ok = store.Cancel("msg1")
	if ok {
		t.Error("second Cancel should return false")
	}
}

func TestSessionCancelAll(t *testing.T) {
	store := NewSessionStore()
	store.Create(context.Background(), "chat1", "msg1", "")
	store.Create(context.Background(), "chat1", "msg2", "")
	store.Create(context.Background(), "chat1", "msg3", "")
	// Cancel one manually
	store.Cancel("msg3")

	count := store.CancelAll("chat1")
	if count != 2 {
		t.Errorf("CancelAll count = %d, want 2", count)
	}
}

func TestSessionMarkDone(t *testing.T) {
	store := NewSessionStore()
	store.Create(context.Background(), "chat1", "msg1", "")
	store.MarkDone("msg1")
	sess := store.Get("msg1")
	if sess.Status != "done" {
		t.Errorf("status = %s, want done", sess.Status)
	}
}

func TestSessionClear(t *testing.T) {
	store := NewSessionStore()
	store.Create(context.Background(), "chat1", "msg1", "")
	store.Create(context.Background(), "chat1", "msg2", "")
	store.Clear("chat1")
	if store.Get("msg1") != nil {
		t.Error("expected Get after Clear to return nil")
	}
}

func TestSessionStats(t *testing.T) {
	store := NewSessionStore()
	store.Create(context.Background(), "chat1", "msg1", "")
	store.Create(context.Background(), "chat1", "msg2", "")
	store.MarkDone("msg2")

	stats := store.Stats("chat1")
	if stats == "" {
		t.Error("expected non-empty stats")
	}
}

func TestSessionResolveParent(t *testing.T) {
	store := NewSessionStore()
	store.Create(context.Background(), "chat1", "root", "")
	store.Create(context.Background(), "chat1", "child", "root")

	parent := store.ResolveParentSession("root")
	if parent == nil {
		t.Error("expected to find parent session")
	}
	if parent.ID != "root" {
		t.Errorf("parent ID = %s, want root", parent.ID)
	}
}
