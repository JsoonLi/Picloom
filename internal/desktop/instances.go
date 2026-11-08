package desktop

import "strings"

type processInfo struct {
	Name        string `json:"Name"`
	PID         int    `json:"ProcessId"`
	CommandLine string `json:"CommandLine"`
}

func conflictingInstance(p processInfo, self int) bool {
	if p.PID == self {
		return false
	}
	if strings.EqualFold(p.Name, "Picloom.exe") || strings.EqualFold(p.Name, "Picloom") || strings.EqualFold(p.Name, "Image2AI.exe") || strings.EqualFold(p.Name, "Image2AI") {
		return true
	}
	command := strings.ToLower(p.CommandLine)
	return (strings.EqualFold(p.Name, "electron.exe") || strings.EqualFold(p.Name, "Electron")) && (strings.Contains(command, "image2ai") || strings.Contains(command, "picloom"))
}
