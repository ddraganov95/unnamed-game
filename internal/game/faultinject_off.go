//go:build !faultinject

package game

// faultInjectChat is a no-op in normal builds. See faultinject_on.go.
func faultInjectChat(string) {}
