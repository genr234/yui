package logging

import (
	"testing"
	"time"
)

type stuckWriter struct{ release chan struct{} }

func (s stuckWriter) Write(p []byte) (int, error) {
	<-s.release
	return len(p), nil
}

func TestAsyncWriterNeverBlocksOnAStuckConsole(t *testing.T) {
	stuck := stuckWriter{release: make(chan struct{})}
	defer close(stuck.release)
	w := newAsyncWriter(stuck, 4)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_, _ = w.Write([]byte("line\n"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writes blocked on a stuck console")
	}
	if w.dropped.Load() == 0 {
		t.Fatal("expected dropped lines to be counted")
	}
}
