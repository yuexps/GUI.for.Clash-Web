package runtime

import (
	"context"
	"sync"
)

var (
	listeners   = make(map[string][]func(...any))
	listenersMu sync.RWMutex

	// OnEmit is called when Go emits an event, so we can broadcast it to frontend via WebSockets
	OnEmit func(event string, data ...any)

	// OnQuit is called when application is exiting
	OnQuit func()
)

func EventsEmit(ctx context.Context, eventName string, optionalData ...any) {
	// 1. Dispatch locally in Go
	listenersMu.RLock()
	cbs, ok := listeners[eventName]
	listenersMu.RUnlock()

	if ok {
		for _, cb := range cbs {
			go cb(optionalData...)
		}
	}

	// 2. Dispatch to Web frontend via WebSocket
	if OnEmit != nil {
		OnEmit(eventName, optionalData...)
	}
}

func EventsOn(ctx context.Context, eventName string, callback func(optionalData ...any)) func() {
	listenersMu.Lock()
	listeners[eventName] = append(listeners[eventName], callback)
	listenersMu.Unlock()

	return func() {
		EventsOff(ctx, eventName)
	}
}

func EventsOff(ctx context.Context, eventName string) {
	listenersMu.Lock()
	delete(listeners, eventName)
	listenersMu.Unlock()
}

func Quit(ctx context.Context) {
	if OnQuit != nil {
		OnQuit()
	}
}

func WindowShow(ctx context.Context)                  {}
func WindowHide(ctx context.Context)                  {}
func WindowSetTitle(ctx context.Context, title string) {}
func InitializeNotifications(ctx context.Context)      {}
func CleanupNotifications(ctx context.Context)         {}
