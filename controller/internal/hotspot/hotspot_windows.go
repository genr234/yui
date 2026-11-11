//go:build windows

package hotspot

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// The WinRT tethering API through PowerShell: no cgo, and the same calls the
// Settings app makes. Parameters travel in environment variables, so names
// and passwords need no quoting.
const prelude = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$ext = [System.WindowsRuntimeSystemExtensions].GetMethods()
$asTask = ($ext | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' })[0]
$asAction = ($ext | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncAction' })[0]
function Await($op) { $t = $asTask.MakeGenericMethod([Windows.Networking.NetworkOperators.NetworkOperatorTetheringOperationResult]).Invoke($null, @($op)); $t.Wait(-1) | Out-Null; $t.Result }
function AwaitAction($op) { $t = $asAction.Invoke($null, @($op)); $t.Wait(-1) | Out-Null }
[Windows.Networking.NetworkOperators.NetworkOperatorTetheringManager,Windows.Networking.NetworkOperators,ContentType=WindowsRuntime] | Out-Null
[Windows.Networking.Connectivity.NetworkInformation,Windows.Networking.Connectivity,ContentType=WindowsRuntime] | Out-Null
$netProfile = [Windows.Networking.Connectivity.NetworkInformation]::GetInternetConnectionProfile()
if ($netProfile -eq $null) { throw 'no internet connection to share' }
$m = [Windows.Networking.NetworkOperators.NetworkOperatorTetheringManager]::CreateFromConnectionProfile($netProfile)
`

const startScript = prelude + `
$c = $m.GetCurrentAccessPointConfiguration()
$c.Ssid = $env:YUI_HOTSPOT_SSID
$c.Passphrase = $env:YUI_HOTSPOT_PASSWORD
$band = 'auto'
try { $c.Band = [Windows.Networking.NetworkOperators.TetheringWiFiBand]::FiveGigahertz; AwaitAction ($m.ConfigureAccessPointAsync($c)); $band = '5ghz' }
catch { $c.Band = [Windows.Networking.NetworkOperators.TetheringWiFiBand]::Auto; AwaitAction ($m.ConfigureAccessPointAsync($c)) }
if ($m.TetheringOperationalState -ne 'On') {
  $r = Await ($m.StartTetheringAsync())
  if ($r.Status -ne 'Success') { throw "start: $($r.Status) $($r.AdditionalErrorMessage)" }
}
"on $band"
`

const stopScript = prelude + `
$c = $m.GetCurrentAccessPointConfiguration()
if ($m.TetheringOperationalState -eq 'Off') { 'off'; return }
if (-not $c.Ssid.StartsWith($env:YUI_HOTSPOT_PREFIX)) { "left on: $($c.Ssid) is not ours"; return }
$r = Await ($m.StopTetheringAsync())
if ($r.Status -ne 'Success') { throw "stop: $($r.Status) $($r.AdditionalErrorMessage)" }
'off'
`

// Start configures the hotspot as ssid/password and turns it on. It tries
// 5 GHz first (less crowded, lower latency) and falls back to the adapter's
// automatic band. The result says which band it got.
func Start(ctx context.Context, ssid, password string) (string, error) {
	return run(ctx, startScript, "YUI_HOTSPOT_SSID="+ssid, "YUI_HOTSPOT_PASSWORD="+password)
}

// Stop turns the hotspot off if this package started it.
func Stop(ctx context.Context) (string, error) {
	return run(ctx, stopScript, "YUI_HOTSPOT_PREFIX="+SSIDPrefix)
}

// Supported reports whether this build can run a hotspot at all.
func Supported() bool { return true }

func run(ctx context.Context, script string, env ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return out, fmt.Errorf("hotspot: %w: %s", err, msg)
	}
	return out, nil
}
