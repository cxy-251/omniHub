package platform

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// Trash uses the platform recycle bin and fails instead of permanently deleting files.
func Trash(path string) error {
	if path == "" {
		return errors.New("trash path is required")
	}
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("gio", "trash", "--", path)
	case "darwin":
		cmd = exec.Command("osascript", "-e", "on run argv\n tell application \"Finder\" to delete (POSIX file (item 1 of argv) as alias)\nend run", path)
	case "windows":
		// The path is passed as data, never interpolated into executable PowerShell source.
		cmd = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop'; Add-Type -AssemblyName Microsoft.VisualBasic; $p=$env:OMNI_TRASH_PATH; if ([System.IO.Directory]::Exists($p)) { [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory($p, 'OnlyErrorDialogs', 'SendToRecycleBin') } else { [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile($p, 'OnlyErrorDialogs', 'SendToRecycleBin') }`)
		cmd.Env = append(os.Environ(), "OMNI_TRASH_PATH="+path)
	default:
		return errors.New("recycle bin is unavailable on this platform")
	}
	return cmd.Run()
}
