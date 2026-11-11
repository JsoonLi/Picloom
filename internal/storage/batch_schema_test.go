package storage

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchPreferencesAreValidated(t *testing.T) {
	valid := []string{
		`{}`,
		`{"batch":{}}`,
		`{"batch":{"concurrency":4,"sizeMode":"fixed","ratio":"16:9","outputDirectory":"` + absoluteForTest() + `"}}`,
	}
	for _, raw := range valid {
		if err := validateConfig([]byte(raw)); err != nil {
			t.Errorf("%s rejected: %v", raw, err)
		}
	}
	invalid := []string{
		`{"batch":[]}`,
		`{"batch":{"concurrency":17}}`,
		`{"batch":{"concurrency":-1}}`,
		`{"batch":{"sizeMode":"stretch"}}`,
		`{"batch":{"ratio":"7:5"}}`,
		`{"batch":{"outputDirectory":"relative/out"}}`,
	}
	for _, raw := range invalid {
		if err := validateConfig([]byte(raw)); err == nil {
			t.Errorf("%s accepted", raw)
		}
	}
}

func absoluteForTest() string {
	if dir, err := filepath.Abs("out"); err == nil {
		return strings.ReplaceAll(dir, "\\", "\\\\")
	}
	return "/out"
}
