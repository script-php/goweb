package server

import "sync/atomic"

// Readiness is shared by the HTTP health handlers and runtime shutdown path.
type Readiness struct{ ready atomic.Bool }

func NewReadiness() *Readiness {
	state := &Readiness{}
	state.ready.Store(true)
	return state
}

func (state *Readiness) IsReady() bool { return state != nil && state.ready.Load() }
func (state *Readiness) SetReady(ready bool) {
	if state != nil {
		state.ready.Store(ready)
	}
}
