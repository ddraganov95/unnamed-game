package safe

import (
	"testing"
	"time"
)

func TestGoRecoversPanicAndCallsOnPanic(t *testing.T) {
	got := make(chan any, 1)
	Go("test", func() { panic("boom") }, func(r any) { got <- r })

	select {
	case r := <-got:
		if r != "boom" {
			t.Fatalf("onPanic got %v, want boom", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onPanic was not called")
	}
}

func TestGoWithoutPanicDoesNotCallOnPanic(t *testing.T) {
	done := make(chan struct{})
	Go("test", func() { close(done) }, func(any) { t.Error("onPanic called without a panic") })

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("fn did not run")
	}
}

func TestCallReportsPanic(t *testing.T) {
	called := false
	if !Call("test", func() { panic("boom") }, func(any) { called = true }) {
		t.Fatal("Call should report the panic")
	}
	if !called {
		t.Fatal("onPanic was not called")
	}
}

func TestCallWithoutPanicReturnsFalse(t *testing.T) {
	ran := false
	if Call("test", func() { ran = true }, nil) {
		t.Fatal("Call reported a panic for normal code")
	}
	if !ran {
		t.Fatal("fn did not run")
	}
}

// A faulty reaction must not take the process down.
func TestFaultyOnPanicDoesNotEscape(t *testing.T) {
	if !Call("test", func() { panic("first") }, func(any) { panic("second") }) {
		t.Fatal("Call should still report the original panic")
	}
}
