package desktop

import "testing"

func TestInstanceDetectionIncludesLegacyDevelopmentHost(t *testing.T) {
	tests := []struct {
		name    string
		process processInfo
		want    bool
	}{
		{"installed", processInfo{Name: "Image2AI.exe", PID: 2}, true},
		{"renamed Windows app", processInfo{Name: "Picloom.exe", PID: 2}, true},
		{"renamed macOS app", processInfo{Name: "Picloom", PID: 2}, true},
		{"renamed self", processInfo{Name: "Picloom.exe", PID: 1}, false},
		{"development", processInfo{Name: "electron.exe", PID: 3, CommandLine: `electron.exe C:/dev/Image2AI`}, true},
		{"mac development", processInfo{Name: "Electron", PID: 4, CommandLine: `/Applications/Electron.app/Contents/MacOS/Electron /code/Image2AI`}, true},
		{"unrelated editor", processInfo{Name: "electron.exe", PID: 5, CommandLine: `electron.exe /code/editor`}, false},
		{"original toolbox", processInfo{Name: "Tools.exe", PID: 6}, false},
		{"self", processInfo{Name: "Image2AI.exe", PID: 1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := conflictingInstance(tt.process, 1); got != tt.want {
				t.Fatalf("conflict=%v want=%v", got, tt.want)
			}
		})
	}
}
