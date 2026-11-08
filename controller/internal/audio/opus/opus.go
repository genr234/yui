// Package opus encodes and decodes Opus with libopus compiled to WebAssembly
// and run under wazero, so the controller keeps building with CGO_ENABLED=0.
//
// The module (wasm/libopus.wasm) is libopus 1.5.2 built by wasm/build.sh.
// It is compiled once per process; every Encoder and Decoder gets its own
// module instance (its own linear memory), so separate instances never share
// state and can run on different goroutines. A single instance is guarded by
// a mutex.
package opus

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed wasm/libopus.wasm
var libopusWasm []byte

// Application selects the libopus coding mode (OPUS_APPLICATION_*).
type Application int32

const (
	AppVoIP               Application = 2048
	AppAudio              Application = 2049
	AppRestrictedLowDelay Application = 2051
)

// CTL request codes from opus_defines.h.
const (
	ctlSetBitrate        = 4002
	ctlSetVBR            = 4006
	ctlSetComplexity     = 4010
	ctlSetInbandFEC      = 4012
	ctlSetPacketLossPerc = 4014
	ctlSetDTX            = 4016
	ctlGetLookahead      = 4027
	ctlSetSignal         = 4024
	ctlGetLastPacketDur  = 4039
)

// Signal hints (OPUS_SIGNAL_*).
const (
	SignalAuto  = -1000
	SignalVoice = 3001
	SignalMusic = 3002
)

// MaxPacket is a safe upper bound for one encoded Opus packet.
const MaxPacket = 1500

// Error is a negative libopus return code.
type Error int32

func (e Error) Error() string {
	switch e {
	case -1:
		return "opus: bad argument"
	case -2:
		return "opus: buffer too small"
	case -3:
		return "opus: internal error"
	case -4:
		return "opus: invalid packet"
	case -5:
		return "opus: unimplemented"
	case -6:
		return "opus: invalid state"
	case -7:
		return "opus: allocation failed"
	}
	return fmt.Sprintf("opus: error %d", int32(e))
}

var errClosed = errors.New("opus: closed")

type shared struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
}

var (
	sharedOnce sync.Once
	sharedVal  *shared
	sharedErr  error
)

func loadShared() (*shared, error) {
	sharedOnce.Do(func() {
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)
		if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
			_ = rt.Close(ctx)
			sharedErr = fmt.Errorf("opus: wasi: %w", err)
			return
		}
		compiled, err := rt.CompileModule(ctx, libopusWasm)
		if err != nil {
			_ = rt.Close(ctx)
			sharedErr = fmt.Errorf("opus: compile: %w", err)
			return
		}
		sharedVal = &shared{rt: rt, compiled: compiled}
	})
	return sharedVal, sharedErr
}

// Version returns libopus's version string, e.g. "libopus 1.5.2".
func Version() (string, error) {
	inst, err := newInstance()
	if err != nil {
		return "", err
	}
	defer inst.close()
	res, err := inst.mod.ExportedFunction("opus_get_version_string").Call(inst.ctx)
	if err != nil {
		return "", err
	}
	return inst.cstring(uint32(res[0])), nil
}

// instance is one libopus module instance plus a scratch area for PCM in,
// packet out and a status word.
type instance struct {
	ctx     context.Context
	mod     api.Module
	malloc  api.Function
	free    api.Function
	scratch uint32
	pcmOff  uint32 // byte offset of the PCM area
	pcmCap  uint32 // bytes
	pktOff  uint32
	errOff  uint32
}

const (
	pcmScratchBytes = 5760 * 2 * 4 // 120 ms of 48 kHz stereo float32
	pktScratchBytes = MaxPacket * 4
)

func newInstance() (*instance, error) {
	sh, err := loadShared()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	cfg := wazero.NewModuleConfig().WithName("").WithStartFunctions("_initialize")
	mod, err := sh.rt.InstantiateModule(ctx, sh.compiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("opus: instantiate: %w", err)
	}
	inst := &instance{
		ctx:    ctx,
		mod:    mod,
		malloc: mod.ExportedFunction("malloc"),
		free:   mod.ExportedFunction("free"),
	}
	size := uint32(pcmScratchBytes + pktScratchBytes + 8)
	res, err := inst.malloc.Call(ctx, uint64(size))
	if err != nil || res[0] == 0 {
		_ = mod.Close(ctx)
		return nil, fmt.Errorf("opus: scratch alloc failed: %v", err)
	}
	inst.scratch = uint32(res[0])
	inst.pcmOff = inst.scratch
	inst.pcmCap = pcmScratchBytes
	inst.pktOff = inst.pcmOff + pcmScratchBytes
	inst.errOff = inst.pktOff + pktScratchBytes
	return inst, nil
}

func (in *instance) close() {
	if in.mod != nil {
		_ = in.mod.Close(in.ctx)
		in.mod = nil
	}
}

func (in *instance) call(name string, args ...uint64) (int32, error) {
	fn := in.mod.ExportedFunction(name)
	if fn == nil {
		return 0, fmt.Errorf("opus: missing export %s", name)
	}
	res, err := fn.Call(in.ctx, args...)
	if err != nil {
		return 0, err
	}
	if len(res) == 0 {
		return 0, nil
	}
	return int32(uint32(res[0])), nil
}

func (in *instance) readI32(off uint32) int32 {
	v, _ := in.mod.Memory().ReadUint32Le(off)
	return int32(v)
}

func (in *instance) cstring(off uint32) string {
	mem := in.mod.Memory()
	var b []byte
	for i := uint32(0); i < 256; i++ {
		c, ok := mem.ReadByte(off + i)
		if !ok || c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

// view returns a writable view of wasm memory. It must be re-fetched after
// any call that may grow memory.
func (in *instance) view(off, n uint32) []byte {
	b, ok := in.mod.Memory().Read(off, n)
	if !ok {
		return nil
	}
	return b
}

func putF32(dst []byte, src []float32) {
	for i, v := range src {
		u := math.Float32bits(v)
		j := i * 4
		dst[j] = byte(u)
		dst[j+1] = byte(u >> 8)
		dst[j+2] = byte(u >> 16)
		dst[j+3] = byte(u >> 24)
	}
}

func getF32(dst []float32, src []byte) {
	for i := range dst {
		j := i * 4
		dst[i] = math.Float32frombits(uint32(src[j]) | uint32(src[j+1])<<8 | uint32(src[j+2])<<16 | uint32(src[j+3])<<24)
	}
}

func putI16(dst []byte, src []int16) {
	for i, v := range src {
		dst[i*2] = byte(v)
		dst[i*2+1] = byte(uint16(v) >> 8)
	}
}

func getI16(dst []int16, src []byte) {
	for i := range dst {
		dst[i] = int16(uint16(src[i*2]) | uint16(src[i*2+1])<<8)
	}
}
