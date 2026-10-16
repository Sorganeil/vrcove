param(
  [string]$Executable = "$PSScriptRoot\dist\vrcove-logger.exe",
  [string]$Output = "$PSScriptRoot\dist\windows-gio.png"
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class LoggerCapture {
  [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left, Top, Right, Bottom; }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out Rect r);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint flags);
}
'@
$process = Start-Process -FilePath $Executable -ArgumentList '--preview' -PassThru
try {
  $deadline = [DateTime]::UtcNow.AddSeconds(20)
  do {
    Start-Sleep -Milliseconds 250
    $process.Refresh()
    if ($process.HasExited) { throw 'Gio preview exited. Check graphics-driver support.' }
  } while ($process.MainWindowHandle -eq [IntPtr]::Zero -and [DateTime]::UtcNow -lt $deadline)
  if ($process.MainWindowHandle -eq [IntPtr]::Zero) { throw 'No logger window appeared.' }
  Start-Sleep -Seconds 2
  $rect = New-Object LoggerCapture+Rect
  if (-not [LoggerCapture]::GetWindowRect($process.MainWindowHandle, [ref]$rect)) { throw 'Could not measure the logger window.' }
  $bitmap = New-Object System.Drawing.Bitmap(($rect.Right-$rect.Left), ($rect.Bottom-$rect.Top))
  try {
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    try {
      $dc = $graphics.GetHdc()
      try {
        if (-not [LoggerCapture]::PrintWindow($process.MainWindowHandle, $dc, 2)) { throw 'Windows could not capture the logger window.' }
      } finally { $graphics.ReleaseHdc($dc) }
    } finally { $graphics.Dispose() }
    $bitmap.Save([System.IO.Path]::GetFullPath($Output), [System.Drawing.Imaging.ImageFormat]::Png)
  } finally { $bitmap.Dispose() }
  Write-Output "Saved $Output"
} finally {
  if (-not $process.HasExited) { $null = $process.CloseMainWindow() }
}
