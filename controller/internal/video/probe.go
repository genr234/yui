package video

import (
	"time"
)

const probeTimeout = 10 * time.Second

type probeResult struct {
	enc encoder
	ok  bool
}

// probe finds the first working encoder of the ladder for a helper binary.
// The result is cached per helper path.
func (s *Service) probe(path string) (encoder, bool) {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	if r, ok := s.probes[path]; ok {
		return r.enc, r.ok
	}
	var res probeResult
	for _, enc := range encoderLadder {
		if s.tryEncoder(path, enc) {
			res = probeResult{enc: enc, ok: true}
			break
		}
	}
	s.probes[path] = res
	return res.enc, res.ok
}

func (s *Service) tryEncoder(path string, enc encoder) bool {
	cmd := s.cfg.Command(path, probeArgs(enc)...)
	if err := cmd.Start(); err != nil {
		return false
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(probeTimeout):
		_ = cmd.Process.Kill()
		<-done
		return false
	}
}

// probedNone reports whether a probe of path ran and found no encoder.
func (s *Service) probedNone(path string) bool {
	// A probe in progress holds the lock; Available must not wait for it.
	if !s.probeMu.TryLock() {
		return false
	}
	defer s.probeMu.Unlock()
	r, ok := s.probes[path]
	return ok && !r.ok
}
