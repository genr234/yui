package video

import (
	"os/exec"
	"strconv"
	"sync"
)

// Encoder is one working entry of the encoder ladder, for other packages
// that encode with the same helper (clips).
type Encoder struct{ enc encoder }

// Name is the ffmpeg encoder name.
func (e Encoder) Name() string { return e.enc.Name }

// Hardware reports whether the encoder runs on the GPU.
func (e Encoder) Hardware() bool { return e.enc.Hardware }

// Args returns the video codec and rate-control options for kbps and fps:
// everything from -c:v on, with a GOP of gop frames (0 = one second).
func (e Encoder) Args(kbps, fps, gop int) []string {
	args := append([]string{}, e.enc.codec(kbps, fps)...)
	rc := common(kbps, fps)
	if gop > 0 {
		rc[1] = strconv.Itoa(gop)
	}
	return append(args, rc...)
}

var (
	findMu    sync.Mutex
	findCache = map[string]probeResult{}
)

// FindEncoder is the first encoder of the ladder that completes a test
// encode with the helper at path, cached per path. command may be nil.
func FindEncoder(command func(name string, args ...string) *exec.Cmd, path string) (Encoder, bool) {
	findMu.Lock()
	defer findMu.Unlock()
	if r, ok := findCache[path]; ok {
		return Encoder{r.enc}, r.ok
	}
	if command == nil {
		command = exec.Command
	}
	s := &Service{cfg: Config{Command: command}}
	var res probeResult
	for _, enc := range encoderLadder {
		if s.tryEncoder(path, enc) {
			res = probeResult{enc: enc, ok: true}
			break
		}
	}
	findCache[path] = res
	return Encoder{res.enc}, res.ok
}
