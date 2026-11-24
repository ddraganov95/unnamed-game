// Package safe runs functions so that a panic is logged with its stack
// instead of crashing the whole server. The caller decides what happens
// after a panic through an onPanic callback (nil means just log).
package safe

import (
	"log/slog"
	"runtime/debug"
)

// Go runs fn in a new goroutine. If fn panics, the panic is logged and
// onPanic(r) is called with the recovered value.
func Go(name string, fn func(), onPanic func(r any)) {
	go Call(name, fn, onPanic)
}

// Call runs fn on the current goroutine with the same recovery as Go. It is
// for code that must keep running after one item fails, such as an event loop
// that handles many independent events. It reports whether fn panicked.
func Call(name string, fn func(), onPanic func(r any)) (panicked bool) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		panicked = true
		slog.Error("panic recovered", "task", name, "panic", r, "stack", string(debug.Stack()))
		if onPanic != nil {
			callOnPanic(name, onPanic, r)
		}
	}()
	fn()
	return false
}

// callOnPanic runs the reaction itself under recovery, so a faulty onPanic
// cannot take the process down.
func callOnPanic(name string, onPanic func(r any), r any) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("panic in panic handler", "task", name, "panic", p, "stack", string(debug.Stack()))
		}
	}()
	onPanic(r)
}
