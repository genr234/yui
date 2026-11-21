<script lang="ts">
  /**
   * Reads the kiosk's QR code with the camera, inside the installed app (the
   * phone's own camera app would open the browser instead). Uses the
   * browser's BarcodeDetector; callers only show it when canScan() is true.
   */
  import { onDestroy, onMount } from 'svelte'

  export let onResult: (text: string) => void
  export let onCancel: () => void

  let video: HTMLVideoElement
  let stream: MediaStream | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  let error = ''
  let stopped = false

  type Detector = { detect(src: CanvasImageSource): Promise<{ rawValue: string }[]> }

  onMount(async () => {
    try {
      const Ctor = (window as unknown as { BarcodeDetector: new (o: { formats: string[] }) => Detector }).BarcodeDetector
      const detector = new Ctor({ formats: ['qr_code'] })
      stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' }, audio: false })
      if (stopped) return stop()
      video.srcObject = stream
      await video.play()
      const tick = async () => {
        if (stopped) return
        try {
          const codes = video.readyState >= 2 ? await detector.detect(video) : []
          if (codes.length && !stopped) {
            onResult(codes[0].rawValue)
            return
          }
        } catch {
          /* a frame that could not be read */
        }
        timer = setTimeout(tick, 150)
      }
      void tick()
    } catch {
      error = "The camera isn't available. Allow camera access, or type the code instead."
    }
  })

  function stop() {
    stopped = true
    clearTimeout(timer)
    stream?.getTracks().forEach((t) => t.stop())
    stream = null
  }

  onDestroy(stop)
</script>

<div class="scan" role="dialog" aria-label="Scan the kiosk's QR code">
  <!-- svelte-ignore a11y-media-has-caption -->
  <video bind:this={video} playsinline muted></video>
  <div class="frame" aria-hidden="true"></div>
  <p>{error || "Point the camera at the QR code on the kiosk's screen."}</p>
  <button class="secondary" type="button" on:click={onCancel}>Cancel</button>
</div>

<style>
  .scan {
    position: fixed;
    inset: 0;
    z-index: 50;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: flex-end;
    gap: 16px;
    padding: 24px 20px calc(24px + env(safe-area-inset-bottom));
    background: #000;
    color: #fff;
    text-align: center;
  }
  video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .frame {
    position: absolute;
    top: 50%;
    left: 50%;
    width: min(70vw, 300px);
    aspect-ratio: 1;
    transform: translate(-50%, -60%);
    border: 4px solid #fff;
    border-radius: 18px;
    box-shadow: 0 0 0 100vmax rgba(0, 0, 0, 0.45);
  }
  p,
  button {
    position: relative;
    max-width: 320px;
  }
  p {
    margin: 0;
    font-weight: 700;
  }
  button {
    width: 100%;
  }
</style>
