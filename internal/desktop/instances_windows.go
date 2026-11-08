//go:build windows

package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Inspect process identity only; never read another application's data.
func CheckOtherInstances() error {
	// Read command lines only for this app's release/development hosts, never environments.
	script := `$ErrorActionPreference='Stop'; $items=@(Get-CimInstance Win32_Process -Filter "Name='Image2AI.exe' OR Name='electron.exe'" | Select-Object Name,ProcessId,CommandLine); ConvertTo-Json -Compress -InputObject $items`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return errors.New("无法检查旧版运行状态。请关闭其他 Image2AI 后重试。")
	}
	var records []processInfo
	if json.Unmarshal(output, &records) != nil {
		return errors.New("无法检查旧版运行状态。请关闭其他 Image2AI 后重试。")
	}
	for _, record := range records {
		if conflictingInstance(record, os.Getpid()) {
			return errors.New("其他 Image2AI 正在运行。请先关闭旧版，再启动原生版本，以保护配置和作品。")
		}
	}
	return nil
}
