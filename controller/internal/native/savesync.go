package native

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

// saveTarget is what a session needs to back up or restore its saves.
type saveTarget struct {
	sessionID string
	appID     string
	runtime   string
	scopeDir  string // <DataDir>/<scope>
	dataDir   string
	saves     []string
}

func (t saveTarget) stageRoot() string { return filepath.Join(t.scopeDir, ".saves") }
func (t saveTarget) marker() string    { return markerPath(t.scopeDir, t.appID, t.runtime) }

// setSaves records the save state on the session it belongs to. A later
// session's status is never touched.
func (m *Manager) setSaves(sessionID string, st SaveStatus) {
	m.mu.Lock()
	apply := m.status.SessionID == sessionID
	if apply {
		cp := st
		m.status.Saves = &cp
	}
	m.mu.Unlock()
	if apply {
		m.publish()
	}
}

// restoreBeforeLaunch brings a newer account backup into the data directory.
// It never fails the launch: problems are logged and reported in Status.Saves.
func (m *Manager) restoreBeforeLaunch(t saveTarget) {
	if len(t.saves) == 0 || m.cfg.SaveRemote == nil {
		return
	}
	m.setSaves(t.sessionID, SaveStatus{State: SavesRestoring})
	st := m.tryRestore(t)
	m.setSaves(t.sessionID, st)
}

func (m *Manager) tryRestore(t saveTarget) (st SaveStatus) {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	fail := func(err error) SaveStatus {
		if errors.Is(err, ErrSavesOffline) {
			return SaveStatus{State: SavesOffline}
		}
		log.Printf("native: restore saves for %s: %v", t.appID, err)
		return SaveStatus{State: SavesError, Error: "could not restore saves: " + publicSaveError(err)}
	}

	info, ok, err := m.cfg.SaveRemote.SaveInfo(t.appID, t.runtime)
	if err != nil {
		return fail(err)
	}
	if !ok {
		return SaveStatus{State: SavesNothing}
	}
	remoteAt := formatSaveTime(info.SavedAt)
	marker := readMarker(t.marker())
	if marker.SHA256 != "" && marker.SHA256 == info.SHA256 {
		return SaveStatus{State: SavesUpToDate, SavedAt: remoteAt}
	}
	if local, has := latestSaveTime(t.dataDir, t.saves); has && local.After(info.SavedAt) {
		log.Printf("native: keeping local saves for %s: newer than the backup (%s > %s)", t.appID, formatSaveTime(local), remoteAt)
		return SaveStatus{State: SavesKeptLocal, SavedAt: formatSaveTime(local)}
	}

	limits := m.cfg.SaveLimits.withDefaults()
	if info.Size > limits.MaxArchiveBytes {
		return fail(ErrTooLarge)
	}
	tmp, err := m.saveTemp(t)
	if err != nil {
		return fail(err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	body, err := m.cfg.SaveRemote.SaveDownload(t.appID, t.runtime)
	if err != nil {
		return fail(err)
	}
	n, err := io.Copy(tmp, io.LimitReader(body, limits.MaxArchiveBytes+1))
	body.Close()
	if err != nil {
		return fail(err)
	}
	if n > limits.MaxArchiveBytes {
		return fail(ErrTooLarge)
	}
	sum, _, err := hashFile(tmp)
	if err != nil {
		return fail(err)
	}
	if info.SHA256 != "" && sum != info.SHA256 {
		return fail(errors.New("downloaded backup does not match its checksum"))
	}
	if err := restoreSaves(tmp.Name(), t.dataDir, t.stageRoot(), t.saves, limits); err != nil {
		return fail(err)
	}
	if err := writeMarker(t.marker(), saveMarker{SHA256: sum, SavedAt: remoteAt}); err != nil {
		log.Printf("native: write saves marker for %s: %v", t.appID, err)
	}
	return SaveStatus{State: SavesRestored, SavedAt: remoteAt}
}

func (m *Manager) saveTemp(t saveTarget) (*os.File, error) {
	if err := os.MkdirAll(t.stageRoot(), 0o755); err != nil {
		return nil, err
	}
	return os.CreateTemp(t.stageRoot(), "backup-*.zip")
}

// saveSnapshot is a packed backup waiting to be uploaded.
type saveSnapshot struct {
	file    *os.File
	sha256  string
	size    int64
	savedAt time.Time
}

func (s *saveSnapshot) close() {
	if s != nil && s.file != nil {
		s.file.Close()
		os.Remove(s.file.Name())
	}
}

// snapshotSaves zips the save paths now. It is quick and local, so the
// session end does it inline and uploads in the background.
func (m *Manager) snapshotSaves(t saveTarget) (*saveSnapshot, error) {
	limits := m.cfg.SaveLimits.withDefaults()
	tmp, err := m.saveTemp(t)
	if err != nil {
		return nil, err
	}
	savedAt, _, err := packSaves(t.dataDir, t.saves, tmp, limits)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	sum, size, err := hashFile(tmp)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	if size > limits.MaxArchiveBytes {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, ErrTooLarge
	}
	return &saveSnapshot{file: tmp, sha256: sum, size: size, savedAt: savedAt}, nil
}

// uploadSnapshot sends the snapshot and records the outcome. It owns snap.
func (m *Manager) uploadSnapshot(t saveTarget, snap *saveSnapshot) SaveStatus {
	defer snap.close()
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	fail := func(err error) SaveStatus {
		if errors.Is(err, ErrSavesOffline) {
			return SaveStatus{State: SavesOffline}
		}
		log.Printf("native: back up saves for %s: %v", t.appID, err)
		return SaveStatus{State: SavesError, Error: "could not back up saves: " + publicSaveError(err), SavedAt: formatSaveTime(snap.savedAt)}
	}
	savedAt := formatSaveTime(snap.savedAt)
	if marker := readMarker(t.marker()); marker.SHA256 == snap.sha256 {
		return SaveStatus{State: SavesSaved, SavedAt: marker.SavedAt}
	}
	if _, err := snap.file.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	err := m.cfg.SaveRemote.SaveUpload(t.appID, t.runtime, SaveUpload{
		Body: snap.file, Size: snap.size, SHA256: snap.sha256, SavedAt: snap.savedAt,
	})
	switch {
	case errors.Is(err, ErrSaveRemoteNewer):
		return SaveStatus{State: SavesRemoteNewr, SavedAt: savedAt}
	case err != nil:
		return fail(err)
	}
	if err := writeMarker(t.marker(), saveMarker{SHA256: snap.sha256, SavedAt: savedAt}); err != nil {
		log.Printf("native: write saves marker for %s: %v", t.appID, err)
	}
	return SaveStatus{State: SavesSaved, SavedAt: savedAt}
}

// backupAfterExit is called when a session's process has ended. The zip is
// made before the data directory is free for the next launch; the upload runs
// in the background and reports through Status.Saves.
func (m *Manager) backupAfterExit(t saveTarget) {
	if len(t.saves) == 0 || m.cfg.SaveRemote == nil {
		return
	}
	snap, err := m.snapshotSaves(t)
	if err != nil {
		m.setSaves(t.sessionID, m.snapshotFailure(t, err))
		return
	}
	m.setSaves(t.sessionID, SaveStatus{State: SavesBackingUp, SavedAt: formatSaveTime(snap.savedAt)})
	m.saveWG.Add(1)
	go func() {
		defer m.saveWG.Done()
		m.setSaves(t.sessionID, m.uploadSnapshot(t, snap))
	}()
}

func (m *Manager) snapshotFailure(t saveTarget, err error) SaveStatus {
	if errors.Is(err, errNoSaves) {
		return SaveStatus{State: SavesNothing}
	}
	log.Printf("native: pack saves for %s: %v", t.appID, err)
	return SaveStatus{State: SavesError, Error: "could not back up saves: " + publicSaveError(err)}
}

// BackupSaves backs up the saves of the current or most recent session
// of appID (native.saves.backup), waiting for the upload. appID, when set,
// must own the session.
func (m *Manager) BackupSaves(appID string) (Status, error) {
	m.mu.Lock()
	t := m.lastSaves
	m.mu.Unlock()
	if t == nil || len(t.saves) == 0 {
		return m.Status(), ErrNoSaveTarget
	}
	if appID != "" && t.appID != appID {
		return m.Status(), ErrNotOwner
	}
	if m.cfg.SaveRemote == nil {
		return m.Status(), userErrorf("save backup is not available")
	}
	m.setSaves(t.sessionID, SaveStatus{State: SavesBackingUp})
	snap, err := m.snapshotSaves(*t)
	if err != nil {
		m.setSaves(t.sessionID, m.snapshotFailure(*t, err))
		return m.Status(), nil
	}
	m.setSaves(t.sessionID, m.uploadSnapshot(*t, snap))
	return m.Status(), nil
}

// waitSaves blocks until background uploads finish or d passes.
func (m *Manager) waitSaves(d time.Duration) {
	done := make(chan struct{})
	go func() {
		m.saveWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
}

func publicSaveError(err error) string {
	switch {
	case errors.Is(err, ErrTooLarge):
		return "the save data is too large"
	case errors.Is(err, ErrTooManyFiles):
		return "the save data has too many files"
	case errors.Is(err, ErrUnsafePath):
		return "the backup contains unsafe paths"
	case errors.Is(err, ErrCorruptArchive):
		return "the backup is corrupt"
	}
	return publicError(err)
}
