package handlers

import "sync"

// Emitter pushes an event frame to every connected overlay.
type Emitter func(event string, data any)

// emitters holds the emitter per Registry. It lives here rather than as a
// Registry field so this file stays independent of registry.go.
var emitters sync.Map // *Registry -> Emitter

// SetEmitter installs the function used by Emit. Passing nil removes it.
// Safe for concurrent use with Emit.
func (r *Registry) SetEmitter(fn func(event string, data any)) {
	if fn == nil {
		emitters.Delete(r)
		return
	}
	emitters.Store(r, Emitter(fn))
}

// Emit pushes a namespaced event (e.g. "assets.changed") to the overlay.
// It is a no-op when no emitter is set.
func (r *Registry) Emit(event string, data any) {
	value, ok := emitters.Load(r)
	if !ok {
		return
	}
	value.(Emitter)(event, data)
}
