package server

import (
	"context"
	"testing"
	"time"
	"uuid"
)

// A panicking DB write must not stop the worker: later writes still run.
func TestDBWriteWorkerSurvivesPanickingTask(t *testing.T) {
	s := &Server{dbWriteChan: make(chan dbWriteTask, 4), dbWriteDone: make(chan struct{})}
	go s.dbWriteWorker()

	ran := make(chan struct{})
	s.enqueueDBWrite("panics", func(context.Context) error { panic("boom") })
	s.enqueueDBWrite("after panic", func(context.Context) error { close(ran); return nil })

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("worker stopped after a panicking task")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.stopDBWrites(ctx)
	select {
	case <-s.dbWriteDone:
	default:
		t.Fatal("worker did not exit after the queue was drained")
	}
}

// Once shutdown has started, new writes are refused rather than panicking on
// a closed channel.
func TestEnqueueDBWriteRefusedAfterStop(t *testing.T) {
	s := &Server{dbWriteChan: make(chan dbWriteTask, 4), dbWriteDone: make(chan struct{})}
	go s.dbWriteWorker()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.stopDBWrites(ctx)

	s.enqueueDBWrite("late", func(context.Context) error { return nil })
	if len(s.dbWriteChan) != 0 {
		t.Fatalf("write queued after stop: len %d", len(s.dbWriteChan))
	}

	ran := false
	s.runAsync("late async", uuid.NewV7(), func(context.Context) error { ran = true; return nil })
	s.asyncWG.Wait()
	if ran {
		t.Fatal("async save started after stop")
	}
}
