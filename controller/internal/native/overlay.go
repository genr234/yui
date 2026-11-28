package native

// While phones calibrate their motion they point at targets the kiosk's page
// draws over the game's area (a QR code to scan, a ring, an edge), and a
// native game's window sits above that page. ShowOverlay(true) brings the
// page (the window that had focus when the session started) to the front;
// ShowOverlay(false) gives the game the front again. The wish outlives
// sessions: a game that starts while someone is calibrating takes the front
// as usual, then the page is lifted over it again.

// ShowOverlay lifts the kiosk's page above the running program, or puts the
// program back on top. It never blocks: window focus moves on its own
// goroutine, in order.
func (m *Manager) ShowOverlay(on bool) {
	m.mu.Lock()
	if m.overlay == on {
		m.mu.Unlock()
		return
	}
	m.overlay = on
	s := m.cur
	running := s != nil && s.cmd != nil
	m.mu.Unlock()
	if running {
		go m.applyOverlay(s, true)
	}
}

func (m *Manager) overlayWanted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.overlay
}

// applyOverlay moves focus to match the current wish, for session s. When
// the game should be in front it is also held there (FocusKeeper), so a tap
// on the page around it cannot leave it behind Chrome. give asks for the
// game to be brought forward first (Launch has already done that).
func (m *Manager) applyOverlay(s *session, give bool) {
	m.focusMu.Lock()
	defer m.focusMu.Unlock()
	select {
	case <-s.focusStop:
		return // the session ended; watch has restored focus already
	default:
	}
	m.releaseHold(s)
	if m.overlayWanted() {
		m.cfg.Focus.Restore(s.focus)
		return
	}
	if give {
		m.cfg.Focus.Give(s.info.PID, s.skip, s.focusStop)
	}
	keeper, ok := m.cfg.Focus.(FocusKeeper)
	if !ok {
		return
	}
	release, done := make(chan struct{}), make(chan struct{})
	s.holdRelease, s.holdDone = release, done
	pid, skip, stop := s.info.PID, s.skip, s.focusStop
	go func() {
		defer close(done)
		keeper.Hold(pid, skip, stop, release)
	}()
}

// releaseHold stops s's focus hold and waits for it, so no later focus move
// races it. The caller holds focusMu.
func (m *Manager) releaseHold(s *session) {
	if s.holdRelease == nil {
		return
	}
	close(s.holdRelease)
	<-s.holdDone
	s.holdRelease, s.holdDone = nil, nil
}
