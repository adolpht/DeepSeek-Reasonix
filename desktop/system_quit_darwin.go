//go:build darwin

package main

/*
#cgo darwin LDFLAGS: -framework Cocoa
void installRexionSystemQuitHook(void);
*/
import "C"

import "sync"

var installSystemQuitHookOnce sync.Once

func installSystemQuitHook() {
	installSystemQuitHookOnce.Do(func() {
		C.installRexionSystemQuitHook()
	})
}

//export RexionMarkSystemQuit
func RexionMarkSystemQuit() {
	markSystemQuitRequested()
}
