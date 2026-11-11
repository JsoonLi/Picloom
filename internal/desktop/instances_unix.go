//go:build !windows

package desktop

import "image2ai/internal/locale"
import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func CheckOtherInstances() error {
	output, err := exec.Command("ps", "-axo", "pid=,comm=,args=").Output()
	if err != nil {
		return errors.New(locale.T("无法检查旧版运行状态。请关闭其他 Picloom 或旧版 Image2AI 后重试。"))
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, _ := strconv.Atoi(fields[0])
		name := filepath.Base(fields[1])
		if conflictingInstance(processInfo{Name: name, PID: pid, CommandLine: strings.Join(fields[2:], " ")}, os.Getpid()) {
			return errors.New(locale.T("其他 Picloom 或旧版 Image2AI 正在运行。请先关闭旧版，再启动原生版本，以保护配置和作品。"))
		}
	}
	return nil
}
