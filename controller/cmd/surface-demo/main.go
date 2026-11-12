// Command surface-demo captures one window of a running program and streams
// it to a browser over a loopback WebRTC link, to measure latency (S1, S3).
//
//	surface-demo --ffmpeg ffmpeg.exe --pid 1234 --title "(?i)notepad"
//
// Open http://127.0.0.1:8099/ in a browser on this PC, or pass --listen
// 0.0.0.0:8099 to reach it from a phone on the same network.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"kiosk/controller/internal/video"
)

const (
	appID     = "demo"
	surfaceID = "demo"
	playerID  = "browser"
)

// counter is the video.RTPWriter the demo hands to the service. It forwards
// to the browser's track and counts bytes for the bitrate printout.
type counter struct {
	mu      sync.Mutex
	track   *webrtc.TrackLocalStaticRTP
	bytes   atomic.Int64
	packets atomic.Int64
}

func (c *counter) WriteRTP(p *rtp.Packet) error {
	c.mu.Lock()
	t := c.track
	c.mu.Unlock()
	if t == nil {
		return nil
	}
	c.bytes.Add(int64(len(p.Payload)))
	c.packets.Add(1)
	return t.WriteRTP(p)
}

func (c *counter) set(t *webrtc.TrackLocalStaticRTP) {
	c.mu.Lock()
	c.track = t
	c.mu.Unlock()
}

func main() {
	ffmpeg := flag.String("ffmpeg", "ffmpeg", "path to the ffmpeg executable")
	title := flag.String("title", "", "RE2 pattern for the window title")
	pid := flag.Int("pid", 0, "process id of the program (its child processes are searched too)")
	listen := flag.String("listen", "127.0.0.1:8099", "address of the local page")
	hide := flag.Bool("hide", false, "move the window off the display")
	relayed := flag.Bool("relayed", false, "use the relayed (low bitrate) tier")
	width := flag.Int("width", 0, "encoded width (default 854)")
	height := flag.Int("height", 0, "encoded height (default 480)")
	flag.Parse()

	w := &counter{}
	svc := video.New(video.Config{
		Helper:  func() (string, error) { return *ffmpeg, nil },
		Writer:  func(string) (video.RTPWriter, bool) { return w, true },
		CanShow: func(string) bool { return true },
		Relayed: func(string) bool { return *relayed },
		Notify: func(p, s string, on bool, wd, ht int) {
			log.Printf("surface %s on=%v %dx%d for %s", s, on, wd, ht, p)
		},
		OnStatus: func(_ string, st []video.Status) {
			b, _ := json.Marshal(st)
			log.Printf("status %s", b)
		},
	})
	defer svc.Close()

	if a := svc.Available(); !a.Available && a.Reason == "unsupported" {
		fmt.Fprintln(os.Stderr, "surface-demo: unsupported on this platform (window capture needs Windows)")
		os.Exit(1)
	}
	if *title == "" || *pid <= 0 {
		fmt.Fprintln(os.Stderr, "surface-demo: --title and --pid are required")
		flag.Usage()
		os.Exit(2)
	}
	svc.SessionStarted(appID, *pid, []video.SurfaceSpec{{
		ID: surfaceID, Title: *title, Hidden: *hide, Width: *width, Height: *height}})

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = rw.Write([]byte(page))
	})
	mux.HandleFunc("/offer", func(rw http.ResponseWriter, r *http.Request) {
		var offer webrtc.SessionDescription
		if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		answer, err := answerOffer(svc, w, offer)
		if err != nil {
			log.Printf("offer: %v", err)
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(rw).Encode(answer)
	})
	srv := &http.Server{Addr: *listen, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	log.Printf("open http://%s/", *listen)
	go report(svc, w)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	_ = srv.Close()
}

// answerOffer creates the browser's peer, waits for the window to be ready and
// shows the surface to it.
func answerOffer(svc *video.Service, w *counter, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
	}, "video", "demo")
	if err != nil {
		return nil, err
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		return nil, err
	}
	go func() { // drain RTCP
		buf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buf); err != nil {
				return
			}
		}
	}()
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("peer %s", s)
		switch s {
		case webrtc.PeerConnectionStateConnected:
			w.set(track)
			svc.PeerChanged(playerID, true)
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected:
			w.set(nil)
			svc.PeerChanged(playerID, false)
		}
	})
	if err := pc.SetRemoteDescription(offer); err != nil {
		return nil, err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, err
	}
	<-gathered

	// Wait for the window, then show.
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err := svc.Show(appID, surfaceID, playerID)
		if err == nil {
			break
		}
		if err != video.ErrNotReady || time.Now().After(deadline) {
			_ = pc.Close()
			return nil, fmt.Errorf("show: %w", err)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return pc.LocalDescription(), nil
}

// report prints rate and frame rate every two seconds while streaming.
func report(svc *video.Service, w *counter) {
	var lastBytes, lastPackets int64
	for range time.Tick(2 * time.Second) {
		b, p := w.bytes.Load(), w.packets.Load()
		if p == lastPackets {
			continue
		}
		fps := 0
		for _, s := range svc.List(appID) {
			fps = s.FPS
		}
		log.Printf("streaming: target %d fps, %.2f Mbit/s, %d packets/s", fps,
			float64(b-lastBytes)*8/2e6, (p-lastPackets)/2)
		lastBytes, lastPackets = b, p
	}
}

const page = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>surface-demo</title>
<style>
 body{margin:0;background:#111;color:#ddd;font:14px system-ui}
 video{width:100%;max-height:85vh;background:#000;object-fit:contain}
 pre{margin:8px;white-space:pre-wrap}
</style>
<video id="v" autoplay muted playsinline></video>
<pre id="s">connecting...</pre>
<script>
(async () => {
  const s = document.getElementById('s'), v = document.getElementById('v');
  const pc = new RTCPeerConnection();
  const tr = pc.addTransceiver('video', {direction: 'recvonly'});
  pc.ontrack = e => {
    v.srcObject = e.streams[0] || new MediaStream([e.track]);
    try { e.receiver.jitterBufferTarget = 0; } catch (_) {}
    try { e.receiver.playoutDelayHint = 0; } catch (_) {}
  };
  await pc.setLocalDescription(await pc.createOffer());
  await new Promise(r => { if (pc.iceGatheringState === 'complete') r(); else
    pc.onicegatheringstatechange = () => pc.iceGatheringState === 'complete' && r(); });
  const res = await fetch('/offer', {method: 'POST', body: JSON.stringify(pc.localDescription)});
  if (!res.ok) { s.textContent = 'error: ' + await res.text(); return; }
  await pc.setRemoteDescription(await res.json());
  let last = null;
  setInterval(async () => {
    for (const r of (await pc.getStats()).values()) {
      if (r.type !== 'inbound-rtp' || r.kind !== 'video') continue;
      const dt = last ? (r.timestamp - last.timestamp) / 1000 : 0;
      const fps = last && dt ? (r.framesDecoded - last.framesDecoded) / dt : 0;
      const mbit = last && dt ? (r.bytesReceived - last.bytesReceived) * 8 / dt / 1e6 : 0;
      s.textContent = r.frameWidth + 'x' + r.frameHeight + '  ' + fps.toFixed(0) + ' fps  ' +
        mbit.toFixed(2) + ' Mbit/s  lost ' + r.packetsLost + '  jitter buffer ' +
        (r.jitterBufferDelay && r.jitterBufferEmittedCount ? (1000 * r.jitterBufferDelay / r.jitterBufferEmittedCount).toFixed(0) : '?') + ' ms';
      last = r;
    }
  }, 1000);
})();
</script>`
