//go:build windows && (amd64 || arm64)

// WASAPI process loopback through a minimal hand-written COM binding (no cgo,
// no go-ole). Written against the documented API and Microsoft's
// ApplicationLoopback sample; it is compile-checked off Windows only and has
// NOT been run on the kiosk. On 64-bit Windows every argument, including the
// REFERENCE_TIME (int64) ones, is passed in one register-sized slot, which is
// why this file is limited to amd64/arm64.

package audio

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modMmdevapi                     = windows.NewLazySystemDLL("Mmdevapi.dll")
	procActivateAudioInterfaceAsync = modMmdevapi.NewProc("ActivateAudioInterfaceAsync")
)

var (
	iidIUnknown              = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIAgileObject          = windows.GUID{Data1: 0x94ea2b94, Data2: 0xe9cc, Data3: 0x49e0, Data4: [8]byte{0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}}
	iidCompletionHandler     = windows.GUID{Data1: 0x41D949AB, Data2: 0x9862, Data3: 0x444A, Data4: [8]byte{0x80, 0xF6, 0xC2, 0x61, 0x33, 0x4D, 0xA5, 0xEB}}
	iidIAudioClient          = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4c32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient   = windows.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48a0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
	virtualProcessLoopback   = "VAD\\Process_Loopback" // VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK
	activationTimeout        = 5 * time.Second
	loopbackBufferDuration   = int64(200000) // 20 ms in 100 ns units, as in Microsoft's sample
	loopbackWaitMilliseconds = uint32(100)
)

const (
	sOK          = 0
	eNoInterface = 0x80004002

	vtBlob = 65 // VT_BLOB

	activationTypeProcessLoopback = 1 // AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK
	loopbackIncludeTree           = 0 // PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE
	loopbackExcludeTree           = 1 // PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE

	shareModeShared         = 0
	streamFlagsLoopback     = 0x00020000
	streamFlagsEventCB      = 0x00040000
	streamFlagsAutoConvert  = 0x80000000
	streamFlagsSRCDefaultQ  = 0x08000000
	bufferFlagsSilent       = 0x2
	waveFormatPCM           = 1
	loopbackRate            = 48000
	loopbackChannels        = 2
	loopbackBitsPerSample   = 16
	loopbackBytesPerFrame   = loopbackChannels * loopbackBitsPerSample / 8
	iAudioClientInitialize  = 3
	iAudioClientStart       = 10
	iAudioClientStop        = 11
	iAudioClientSetEvent    = 13
	iAudioClientGetService  = 14
	iCaptureGetBuffer       = 3
	iCaptureReleaseBuffer   = 4
	iCaptureNextPacketSize  = 5
	iActivateOpGetResult    = 3
	iUnknownRelease         = 2
	maxVtableSlotsWeIndex   = 16
	waitObject0             = 0
	waitTimeout             = 0x102
	activateCompletedMethod = 3
)

// AUDIOCLIENT_ACTIVATION_PARAMS with its PROCESS_LOOPBACK_PARAMS union arm.
type activationParams struct {
	ActivationType      uint32
	TargetProcessID     uint32
	ProcessLoopbackMode uint32
}

// PROPVARIANT holding a BLOB (64-bit layout: 8-byte header, cbSize, pad,
// pointer; 24 bytes).
type propVariantBlob struct {
	vt        uint16
	reserved1 uint16
	reserved2 uint16
	reserved3 uint16
	cbSize    uint32
	pBlobData *activationParams
}

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	Size           uint16
}

// comObj is any COM interface pointer: its first word points at the vtable.
type comObj struct {
	vtbl *[maxVtableSlotsWeIndex]uintptr
}

func (o *comObj) call(slot int, args ...uintptr) int32 {
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(unsafe.Pointer(o)))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(o.vtbl[slot], all...)
	return int32(uint32(r))
}

func (o *comObj) release() {
	if o != nil {
		o.call(iUnknownRelease)
	}
}

func hrErr(what string, hr int32) error {
	return fmt.Errorf("audio: %s failed: HRESULT 0x%08X", what, uint32(hr))
}

// completionHandler implements IActivateAudioInterfaceCompletionHandler and
// IAgileObject (activation requires an agile handler).
type completionHandler struct {
	vtbl *[4]uintptr
	refs int32
	once sync.Once
	done chan struct{}
}

var (
	handlerVtblOnce sync.Once
	handlerVtbl     [4]uintptr
	// liveHandlers keeps handlers reachable while Windows may still call
	// them (e.g. after an activation timeout).
	liveHandlersMu sync.Mutex
	liveHandlers   = map[*completionHandler]struct{}{}
)

func newCompletionHandler() *completionHandler {
	handlerVtblOnce.Do(func() {
		handlerVtbl[0] = windows.NewCallback(handlerQueryInterface)
		handlerVtbl[1] = windows.NewCallback(handlerAddRef)
		handlerVtbl[2] = windows.NewCallback(handlerRelease)
		handlerVtbl[activateCompletedMethod] = windows.NewCallback(handlerActivateCompleted)
	})
	h := &completionHandler{vtbl: &handlerVtbl, refs: 1, done: make(chan struct{})}
	liveHandlersMu.Lock()
	liveHandlers[h] = struct{}{}
	liveHandlersMu.Unlock()
	return h
}

func handlerQueryInterface(this *completionHandler, riid *windows.GUID, ppv *uintptr) uintptr {
	if ppv == nil {
		return 0x80004003 // E_POINTER
	}
	if riid != nil && (*riid == iidIUnknown || *riid == iidCompletionHandler || *riid == iidIAgileObject) {
		*ppv = uintptr(unsafe.Pointer(this))
		atomic.AddInt32(&this.refs, 1)
		return sOK
	}
	*ppv = 0
	return eNoInterface
}

func handlerAddRef(this *completionHandler) uintptr {
	return uintptr(atomic.AddInt32(&this.refs, 1))
}

func handlerRelease(this *completionHandler) uintptr {
	n := atomic.AddInt32(&this.refs, -1)
	if n <= 0 {
		liveHandlersMu.Lock()
		delete(liveHandlers, this)
		liveHandlersMu.Unlock()
	}
	return uintptr(n)
}

func handlerActivateCompleted(this *completionHandler, _ uintptr) uintptr {
	this.once.Do(func() { close(this.done) })
	return sOK
}

func (p *ProcessLoopback) start(ctx context.Context) (<-chan Chunk, error) {
	if p.PID == 0 {
		return nil, fmt.Errorf("audio: process loopback needs a PID")
	}
	if err := procActivateAudioInterfaceAsync.Find(); err != nil {
		return nil, fmt.Errorf("audio: %w: %v", ErrLoopbackUnsupported, err)
	}
	out := make(chan Chunk, 8)
	ready := make(chan error, 1)
	go p.run(ctx, out, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return out, nil
}

// run owns one OS thread for the whole COM lifetime (MTA).
func (p *ProcessLoopback) run(ctx context.Context, out chan<- Chunk, ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	started := false
	defer func() {
		if started {
			close(out)
		}
	}()

	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
		ready <- fmt.Errorf("audio: CoInitializeEx: %w", err)
		return
	}
	defer windows.CoUninitialize()

	client, err := p.activate(ctx)
	if err != nil {
		ready <- err
		return
	}
	defer client.release()

	format := waveFormatEx{
		FormatTag:      waveFormatPCM,
		Channels:       loopbackChannels,
		SamplesPerSec:  loopbackRate,
		AvgBytesPerSec: loopbackRate * loopbackBytesPerFrame,
		BlockAlign:     loopbackBytesPerFrame,
		BitsPerSample:  loopbackBitsPerSample,
	}
	flags := uintptr(streamFlagsLoopback | streamFlagsEventCB | streamFlagsAutoConvert | streamFlagsSRCDefaultQ)
	if hr := client.call(iAudioClientInitialize, shareModeShared, flags, uintptr(loopbackBufferDuration), 0, uintptr(unsafe.Pointer(&format)), 0); hr < 0 {
		ready <- hrErr("IAudioClient::Initialize", hr)
		return
	}
	runtime.KeepAlive(&format)

	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		ready <- fmt.Errorf("audio: CreateEvent: %w", err)
		return
	}
	defer windows.CloseHandle(event)
	if hr := client.call(iAudioClientSetEvent, uintptr(event)); hr < 0 {
		ready <- hrErr("IAudioClient::SetEventHandle", hr)
		return
	}

	var capture *comObj
	if hr := client.call(iAudioClientGetService, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&capture))); hr < 0 || capture == nil {
		ready <- hrErr("IAudioClient::GetService(IAudioCaptureClient)", hr)
		return
	}
	defer capture.release()

	if hr := client.call(iAudioClientStart); hr < 0 {
		ready <- hrErr("IAudioClient::Start", hr)
		return
	}
	defer client.call(iAudioClientStop)

	started = true
	ready <- nil

	for {
		if ctx.Err() != nil {
			return
		}
		ev, err := windows.WaitForSingleObject(event, loopbackWaitMilliseconds)
		if err != nil {
			p.setErr(fmt.Errorf("audio: WaitForSingleObject: %w", err))
			return
		}
		if ev != waitObject0 && ev != waitTimeout {
			p.setErr(fmt.Errorf("audio: WaitForSingleObject returned %d", ev))
			return
		}
		if err := p.drain(ctx, capture, out); err != nil {
			p.setErr(err)
			return
		}
	}
}

// activate calls ActivateAudioInterfaceAsync for the process loopback
// virtual device and waits for the completion handler.
func (p *ProcessLoopback) activate(ctx context.Context) (*comObj, error) {
	mode := uint32(loopbackIncludeTree)
	if p.ExcludeTree {
		mode = loopbackExcludeTree
	}
	params := &activationParams{
		ActivationType:      activationTypeProcessLoopback,
		TargetProcessID:     p.PID,
		ProcessLoopbackMode: mode,
	}
	pv := &propVariantBlob{vt: vtBlob, cbSize: uint32(unsafe.Sizeof(*params)), pBlobData: params}
	path, err := windows.UTF16PtrFromString(virtualProcessLoopback)
	if err != nil {
		return nil, err
	}
	h := newCompletionHandler()
	var op *comObj
	r, _, _ := procActivateAudioInterfaceAsync.Call(
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&iidIAudioClient)),
		uintptr(unsafe.Pointer(pv)),
		uintptr(unsafe.Pointer(h)),
		uintptr(unsafe.Pointer(&op)),
	)
	if hr := int32(uint32(r)); hr < 0 {
		handlerRelease(h)
		return nil, hrErr("ActivateAudioInterfaceAsync", hr)
	}
	defer op.release()

	timer := time.NewTimer(activationTimeout)
	defer timer.Stop()
	select {
	case <-h.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("audio: process loopback activation timed out after %s", activationTimeout)
	}
	runtime.KeepAlive(params)
	runtime.KeepAlive(pv)
	runtime.KeepAlive(path)
	handlerRelease(h)

	var activateHR int32
	var client *comObj
	if hr := op.call(iActivateOpGetResult, uintptr(unsafe.Pointer(&activateHR)), uintptr(unsafe.Pointer(&client))); hr < 0 {
		return nil, hrErr("GetActivateResult", hr)
	}
	if activateHR < 0 || client == nil {
		client.release()
		return nil, hrErr(fmt.Sprintf("process loopback activation for PID %d", p.PID), activateHR)
	}
	return client, nil
}

// drain reads every packet currently queued in the capture client.
func (p *ProcessLoopback) drain(ctx context.Context, capture *comObj, out chan<- Chunk) error {
	for {
		var packet uint32
		if hr := capture.call(iCaptureNextPacketSize, uintptr(unsafe.Pointer(&packet))); hr < 0 {
			return hrErr("IAudioCaptureClient::GetNextPacketSize", hr)
		}
		if packet == 0 {
			return nil
		}
		var data *byte
		var frames, flags uint32
		if hr := capture.call(iCaptureGetBuffer,
			uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, 0); hr < 0 {
			return hrErr("IAudioCaptureClient::GetBuffer", hr)
		}
		samples := make([]float32, 0, int(frames)*loopbackChannels)
		if flags&bufferFlagsSilent != 0 || data == nil {
			samples = samples[:int(frames)*loopbackChannels]
		} else {
			raw := unsafe.Slice(data, int(frames)*loopbackBytesPerFrame)
			samples = decodePCM(samples, raw, S16LE)
		}
		if hr := capture.call(iCaptureReleaseBuffer, uintptr(frames)); hr < 0 {
			return hrErr("IAudioCaptureClient::ReleaseBuffer", hr)
		}
		if len(samples) == 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case out <- Chunk{SampleRate: loopbackRate, Channels: loopbackChannels, Samples: samples}:
		}
	}
}
