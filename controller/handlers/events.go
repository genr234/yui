package handlers

// Emitter pushes an event frame to every connected overlay.
type Emitter func(event string, data any)

// SetEmitter installs the function used by Emit. Passing nil removes it.
// Safe for concurrent use with Emit.
func (r *Registry) SetEmitter(fn func(event string, data any)) {
	r.emitMu.Lock()
	defer r.emitMu.Unlock()
	r.emitter = fn
}

// Emit pushes a namespaced event (e.g. "assets.changed") to the overlay.
// It is a no-op when no emitter is set.
func (r *Registry) Emit(event string, data any) {
	r.emitMu.RLock()
	fn := r.emitter
	r.emitMu.RUnlock()
	if fn != nil {
		fn(event, data)
	}
}
