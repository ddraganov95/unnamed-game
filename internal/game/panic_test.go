package game

import (
	"testing"
	"time"
)

// A panic in a game goroutine must end only that game, with the "panic"
// destroy reason, through the normal path (GameDestroy then DeactivateGame).
func TestGoSafePanicDestroysGameWithPanicReason(t *testing.T) {
	g := newTestGame(t)

	reasons := make(chan string, 1)
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		for {
			select {
			case <-quit:
				return
			case ev := <-g.ServerEventChan:
				if ev.Type == EventTypeGameDestroy {
					reasons <- ev.Value
					ev.RespChan <- nil
				}
			}
		}
	}()

	g.goSafe("test goroutine", func() { panic("boom") })

	select {
	case <-g.DestroyChan:
	case <-time.After(3 * time.Second):
		t.Fatal("game was not destroyed after a panic in its goroutine")
	}
	select {
	case reason := <-reasons:
		if reason != DestroyReasonPanic.String() {
			t.Fatalf("destroy reason %q, want %q", reason, DestroyReasonPanic.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no GameDestroy event sent after the panic")
	}
}
