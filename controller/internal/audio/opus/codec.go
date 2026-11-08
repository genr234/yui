package opus

import (
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero/api"
)

// Encoder wraps an OpusEncoder. Methods are safe for concurrent use.
type Encoder struct {
	mu          sync.Mutex
	in          *instance
	st          uint32
	channels    int
	sampleRate  int
	encode      api.Function
	encodeFloat api.Function
}

// NewEncoder creates an encoder. sampleRate must be 8000, 12000, 16000,
// 24000 or 48000; channels 1 or 2.
func NewEncoder(sampleRate, channels int, app Application) (*Encoder, error) {
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("opus: channels must be 1 or 2, got %d", channels)
	}
	in, err := newInstance()
	if err != nil {
		return nil, err
	}
	st, err := in.call("opus_encoder_create", uint64(sampleRate), uint64(channels), uint64(uint32(app)), uint64(in.errOff))
	if err != nil {
		in.close()
		return nil, err
	}
	if code := in.readI32(in.errOff); code != 0 || st == 0 {
		in.close()
		return nil, Error(code)
	}
	return &Encoder{
		in:          in,
		st:          uint32(st),
		channels:    channels,
		sampleRate:  sampleRate,
		encode:      in.mod.ExportedFunction("opus_encode"),
		encodeFloat: in.mod.ExportedFunction("opus_encode_float"),
	}, nil
}

// EncodeFloat encodes one frame of interleaved float32 PCM (nominal range
// [-1, 1]) into out and returns the packet length. The frame must be a legal
// Opus duration (2.5, 5, 10, 20, 40 or 60 ms).
func (e *Encoder) EncodeFloat(pcm []float32, out []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.in == nil {
		return 0, errClosed
	}
	n := uint32(len(pcm) * 4)
	if len(pcm)%e.channels != 0 || n > e.in.pcmCap {
		return 0, fmt.Errorf("opus: bad frame length %d", len(pcm))
	}
	putF32(e.in.view(e.in.pcmOff, n), pcm)
	return e.finish(e.encodeFloat, len(pcm)/e.channels, out)
}

// Encode is EncodeFloat for int16 PCM.
func (e *Encoder) Encode(pcm []int16, out []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.in == nil {
		return 0, errClosed
	}
	n := uint32(len(pcm) * 2)
	if len(pcm)%e.channels != 0 || n > e.in.pcmCap {
		return 0, fmt.Errorf("opus: bad frame length %d", len(pcm))
	}
	putI16(e.in.view(e.in.pcmOff, n), pcm)
	return e.finish(e.encode, len(pcm)/e.channels, out)
}

func (e *Encoder) finish(fn api.Function, frameSize int, out []byte) (int, error) {
	max := len(out)
	if max > pktScratchBytes {
		max = pktScratchBytes
	}
	res, err := fn.Call(e.in.ctx, uint64(e.st), uint64(e.in.pcmOff), uint64(frameSize), uint64(e.in.pktOff), uint64(max))
	if err != nil {
		return 0, err
	}
	n := int32(uint32(res[0]))
	if n < 0 {
		return 0, Error(n)
	}
	copy(out, e.in.view(e.in.pktOff, uint32(n)))
	return int(n), nil
}

func (e *Encoder) set(request, value int32) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.in == nil {
		return errClosed
	}
	code, err := e.in.call("yui_encoder_set", uint64(e.st), uint64(uint32(request)), uint64(uint32(value)))
	if err != nil {
		return err
	}
	if code != 0 {
		return Error(code)
	}
	return nil
}

func (e *Encoder) get(request int32) (int32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.in == nil {
		return 0, errClosed
	}
	code, err := e.in.call("yui_encoder_get", uint64(e.st), uint64(uint32(request)), uint64(e.in.errOff))
	if err != nil {
		return 0, err
	}
	if code != 0 {
		return 0, Error(code)
	}
	return e.in.readI32(e.in.errOff), nil
}

func boolInt(v bool) int32 {
	if v {
		return 1
	}
	return 0
}

// SetBitrate sets the target bitrate in bits per second.
func (e *Encoder) SetBitrate(bps int) error { return e.set(ctlSetBitrate, int32(bps)) }

// SetComplexity sets the CPU/quality trade-off, 0 to 10.
func (e *Encoder) SetComplexity(c int) error { return e.set(ctlSetComplexity, int32(c)) }

// SetVBR enables or disables variable bitrate.
func (e *Encoder) SetVBR(on bool) error { return e.set(ctlSetVBR, boolInt(on)) }

// SetInBandFEC enables SILK in-band forward error correction.
func (e *Encoder) SetInBandFEC(on bool) error { return e.set(ctlSetInbandFEC, boolInt(on)) }

// SetPacketLossPerc tells the encoder the expected loss percentage.
func (e *Encoder) SetPacketLossPerc(p int) error { return e.set(ctlSetPacketLossPerc, int32(p)) }

// SetDTX enables discontinuous transmission.
func (e *Encoder) SetDTX(on bool) error { return e.set(ctlSetDTX, boolInt(on)) }

// SetSignal sets the signal-type hint (SignalAuto, SignalVoice, SignalMusic).
func (e *Encoder) SetSignal(s int) error { return e.set(ctlSetSignal, int32(s)) }

// Lookahead returns the encoder's algorithmic delay in samples at its rate.
func (e *Encoder) Lookahead() (int, error) {
	v, err := e.get(ctlGetLookahead)
	return int(v), err
}

// Reset clears the encoder state (as if freshly created, same settings).
func (e *Encoder) Reset() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.in == nil {
		return errClosed
	}
	code, err := e.in.call("yui_encoder_reset", uint64(e.st))
	if err != nil {
		return err
	}
	if code != 0 {
		return Error(code)
	}
	return nil
}

// Close releases the module instance.
func (e *Encoder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.in != nil {
		e.in.close()
		e.in = nil
	}
	return nil
}

// Decoder wraps an OpusDecoder. Methods are safe for concurrent use.
type Decoder struct {
	mu          sync.Mutex
	in          *instance
	st          uint32
	channels    int
	decode      api.Function
	decodeFloat api.Function
}

// NewDecoder creates a decoder producing PCM at sampleRate with channels.
func NewDecoder(sampleRate, channels int) (*Decoder, error) {
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("opus: channels must be 1 or 2, got %d", channels)
	}
	in, err := newInstance()
	if err != nil {
		return nil, err
	}
	st, err := in.call("opus_decoder_create", uint64(sampleRate), uint64(channels), uint64(in.errOff))
	if err != nil {
		in.close()
		return nil, err
	}
	if code := in.readI32(in.errOff); code != 0 || st == 0 {
		in.close()
		return nil, Error(code)
	}
	return &Decoder{
		in:          in,
		st:          uint32(st),
		channels:    channels,
		decode:      in.mod.ExportedFunction("opus_decode"),
		decodeFloat: in.mod.ExportedFunction("opus_decode_float"),
	}, nil
}

// DecodeFloat decodes one packet into interleaved float32 pcm and returns
// the number of samples per channel. A nil packet runs packet-loss
// concealment for len(pcm)/channels samples.
func (d *Decoder) DecodeFloat(packet []byte, pcm []float32) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n, err := d.run(d.decodeFloat, packet, len(pcm), 4)
	if err != nil {
		return 0, err
	}
	getF32(pcm[:n*d.channels], d.in.view(d.in.pcmOff, uint32(n*d.channels*4)))
	return n, nil
}

// Decode is DecodeFloat for int16 PCM.
func (d *Decoder) Decode(packet []byte, pcm []int16) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n, err := d.run(d.decode, packet, len(pcm), 2)
	if err != nil {
		return 0, err
	}
	getI16(pcm[:n*d.channels], d.in.view(d.in.pcmOff, uint32(n*d.channels*2)))
	return n, nil
}

func (d *Decoder) run(fn api.Function, packet []byte, pcmLen, sampleBytes int) (int, error) {
	if d.in == nil {
		return 0, errClosed
	}
	if len(packet) > pktScratchBytes {
		return 0, fmt.Errorf("opus: packet too large (%d bytes)", len(packet))
	}
	frameSize := pcmLen / d.channels
	if max := int(d.in.pcmCap) / (d.channels * sampleBytes); frameSize > max {
		frameSize = max
	}
	var dataOff, dataLen uint64
	if packet != nil {
		copy(d.in.view(d.in.pktOff, uint32(len(packet))), packet)
		dataOff, dataLen = uint64(d.in.pktOff), uint64(len(packet))
	}
	res, err := fn.Call(d.in.ctx, uint64(d.st), dataOff, dataLen, uint64(d.in.pcmOff), uint64(frameSize), 0)
	if err != nil {
		return 0, err
	}
	n := int32(uint32(res[0]))
	if n < 0 {
		return 0, Error(n)
	}
	return int(n), nil
}

// LastPacketDuration returns the duration in samples of the last decoded
// packet.
func (d *Decoder) LastPacketDuration() (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.in == nil {
		return 0, errClosed
	}
	code, err := d.in.call("yui_decoder_get", uint64(d.st), ctlGetLastPacketDur, uint64(d.in.errOff))
	if err != nil {
		return 0, err
	}
	if code != 0 {
		return 0, Error(code)
	}
	return int(d.in.readI32(d.in.errOff)), nil
}

// Close releases the module instance.
func (d *Decoder) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.in != nil {
		d.in.close()
		d.in = nil
	}
	return nil
}
