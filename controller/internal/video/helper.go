package video

import (
	"strconv"
	"strings"
)

// ffmpegNames holds every ffmpeg filter, option and encoder name this package
// relies on. Verify with S1 (`ffmpeg -h filter=gfxcapture`, `ffmpeg -encoders`);
// a wrong name is fixed here and nowhere else.
var ffmpegNames = struct {
	CaptureFilter string // source filter that captures one window
	HwndOpt       string
	FramerateOpt  string
	CursorOpt     string
	Download      string // moves captured frames from the GPU to system memory
	PixelIn       string // pixel format the capture filter delivers after Download
	PixelOut      string // pixel format the encoders get
	Format        string // output muxer
	ExtraBSF      string // repeats SPS/PPS in front of every keyframe
}{
	CaptureFilter: "gfxcapture",
	HwndOpt:       "hwnd",
	FramerateOpt:  "max_framerate",
	CursorOpt:     "capture_cursor",
	Download:      "hwdownload",
	PixelIn:       "bgra",
	PixelOut:      "yuv420p",
	Format:        "rtp",
	ExtraBSF:      "dump_extra=freq=keyframe",
}

// packetSize is the RTP payload budget, below a typical tunnelled MTU.
const packetSize = 1200

// Tier parameters (W5).
const (
	directKbps    = 3000
	relayedKbps   = 1200
	hardwareFPS   = 60
	softwareFPS   = 30
	relayedFPS    = 30
	probeKbps     = 3000
	probeFPS      = 30
	probeDuration = "0.5"
)

// encoder is one entry of the encoder ladder.
type encoder struct {
	Name     string // ffmpeg encoder name
	Hardware bool
	// codec returns the encoder-specific options (everything after -c:v).
	codec func(kbps, fps int) []string
}

func kb(n int) string { return strconv.Itoa(n) + "k" }

// encoderLadder is tried in order; the first one that completes a test
// encode is used (W5). Option names: verify with S1.
var encoderLadder = []encoder{
	{Name: "h264_nvenc", Hardware: true, codec: func(kbps, fps int) []string {
		return []string{"-c:v", "h264_nvenc", "-preset", "p1", "-tune", "ll", "-zerolatency", "1",
			"-profile:v", "baseline", "-level", "3.1", "-rc", "cbr", "-forced-idr", "1"}
	}},
	{Name: "h264_qsv", Hardware: true, codec: func(kbps, fps int) []string {
		return []string{"-c:v", "h264_qsv", "-preset", "veryfast", "-profile:v", "baseline",
			"-level", "3.1", "-look_ahead", "0", "-low_power", "1"}
	}},
	{Name: "h264_amf", Hardware: true, codec: func(kbps, fps int) []string {
		return []string{"-c:v", "h264_amf", "-usage", "ultralowlatency", "-profile:v", "constrained_baseline",
			"-level", "3.1", "-rc", "cbr", "-quality", "speed"}
	}},
	{Name: "h264_mf", Hardware: true, codec: func(kbps, fps int) []string {
		return []string{"-c:v", "h264_mf", "-hw_encoding", "1", "-rate_control", "cbr",
			"-scenario", "live_streaming", "-profile:v", "baseline"}
	}},
	{Name: "libx264", Hardware: false, codec: func(kbps, fps int) []string {
		return []string{"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
			"-profile:v", "baseline", "-level", "3.1",
			"-x264-params", "scenecut=0:repeat-headers=1"}
	}},
}

// tierParams picks bitrate and frame rate for an encoder and link tier.
func tierParams(enc encoder, relayed bool) (kbps, fps int) {
	switch {
	case relayed:
		return relayedKbps, relayedFPS
	case enc.Hardware:
		return directKbps, hardwareFPS
	default:
		return directKbps, softwareFPS
	}
}

// common returns the rate-control options shared by every encoder: constant
// bitrate with a half-second VBV, a one-second GOP and no B-frames.
func common(kbps, fps int) []string {
	return []string{"-g", strconv.Itoa(fps), "-bf", "0",
		"-b:v", kb(kbps), "-maxrate", kb(kbps), "-bufsize", kb(kbps / 2)}
}

type helperOpts struct {
	Hwnd    uintptr
	Width   int
	Height  int
	FPS     int
	Kbps    int
	Encoder encoder
	Port    int // loopback UDP port the RTP goes to
}

// helperArgs builds the capture-and-encode command line. Pure; golden-tested.
func helperArgs(o helperOpts) []string {
	n := ffmpegNames
	src := n.CaptureFilter + "=" + strings.Join([]string{
		n.HwndOpt + "=" + strconv.FormatUint(uint64(o.Hwnd), 10),
		n.FramerateOpt + "=" + strconv.Itoa(o.FPS),
		n.CursorOpt + "=0",
	}, ":") + "," + n.Download + ",format=" + n.PixelIn
	// fps= repeats the last frame so the encoder sees a constant rate even
	// when the window does not change.
	vf := "fps=" + strconv.Itoa(o.FPS) + ",scale=" + strconv.Itoa(o.Width) + ":" + strconv.Itoa(o.Height) +
		":flags=fast_bilinear,format=" + n.PixelOut
	args := []string{"-hide_banner", "-loglevel", "warning", "-nostats",
		"-f", "lavfi", "-i", src, "-vf", vf, "-an"}
	args = append(args, o.Encoder.codec(o.Kbps, o.FPS)...)
	args = append(args, common(o.Kbps, o.FPS)...)
	args = append(args, "-bsf:v", n.ExtraBSF, "-flush_packets", "1",
		"-f", n.Format, "rtp://127.0.0.1:"+strconv.Itoa(o.Port)+"?pkt_size="+strconv.Itoa(packetSize))
	return args
}

// probeArgs builds a half-second test encode of a synthetic source.
func probeArgs(enc encoder) []string {
	args := []string{"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=854x480:rate=" + strconv.Itoa(probeFPS),
		"-t", probeDuration, "-pix_fmt", ffmpegNames.PixelOut, "-an"}
	args = append(args, enc.codec(probeKbps, probeFPS)...)
	args = append(args, common(probeKbps, probeFPS)...)
	return append(args, "-f", "null", "-")
}
