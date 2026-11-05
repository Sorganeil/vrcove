package sensors

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createNoWindow stops helper programs flashing a console.
const createNoWindow = 0x08000000

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	getSystemTimes       = kernel32.NewProc("GetSystemTimes")
	globalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

func helper(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	return cmd
}

// nowPlayingScript asks Windows' media session manager, which Go can't reach
// without a WinRT binding. It prints nothing unless something is playing.
const nowPlayingScript = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$asTask = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
  $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and
  $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1'
} | Select-Object -First 1
function Await($operation, $type) {
  $task = $asTask.MakeGenericMethod($type).Invoke($null, @($operation))
  $task.Wait(3000) | Out-Null
  $task.Result
}
$manager = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType = WindowsRuntime]
$properties = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties, Windows.Media.Control, ContentType = WindowsRuntime]
$session = (Await $manager::RequestAsync() $manager).GetCurrentSession()
if (-not $session) { exit }
if ($session.GetPlaybackInfo().PlaybackStatus -ne 'Playing') { exit }
$media = Await $session.TryGetMediaPropertiesAsync() $properties
@{ title = $media.Title; artist = $media.Artist } | ConvertTo-Json -Compress
`

// NowPlaying reads the system's current media session.
func NowPlaying(ctx context.Context) Media {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	encoded := base64.StdEncoding.EncodeToString(utf16Bytes(nowPlayingScript))
	output, err := helper(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded).Output()
	if err != nil {
		return Media{}
	}
	return parseNowPlaying(output)
}

func parseNowPlaying(output []byte) Media {
	var media struct{ Title, Artist string }
	if json.Unmarshal(output, &media) != nil {
		return Media{}
	}
	return Media{strings.TrimSpace(media.Title), strings.TrimSpace(media.Artist)}
}

// utf16Bytes encodes text as PowerShell's -EncodedCommand expects.
func utf16Bytes(text string) []byte {
	units := utf16.Encode([]rune(text))
	encoded := make([]byte, 0, 2*len(units))
	for _, unit := range units {
		encoded = append(encoded, byte(unit), byte(unit>>8))
	}
	return encoded
}

type memoryStatus struct {
	length, memoryLoad                                 uint32
	totalPhys, availPhys, totalPageFile, availPageFile uint64
	totalVirtual, availVirtual, availExtendedVirtual   uint64
}

func readUsage() (busy, idle uint64, ram int, ok bool) {
	var idleTime, kernelTime, userTime windows.Filetime
	result, _, _ := getSystemTimes.Call(uintptr(unsafe.Pointer(&idleTime)), uintptr(unsafe.Pointer(&kernelTime)), uintptr(unsafe.Pointer(&userTime)))
	if result == 0 {
		return 0, 0, 0, false
	}
	status := memoryStatus{length: uint32(unsafe.Sizeof(memoryStatus{}))}
	result, _, _ = globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		return 0, 0, 0, false
	}
	// Kernel time includes idle time.
	idle = ticks(idleTime)
	return ticks(kernelTime) + ticks(userTime) - idle, idle, int(status.memoryLoad), true
}

func ticks(t windows.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }
