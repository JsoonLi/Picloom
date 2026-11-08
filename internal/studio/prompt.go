package studio

import (
	"strings"
	"unicode/utf8"
)

// compactPrompt removes whitespace-only lines, mapping rune-based selection
// offsets so cleanup does not move typing past the text after a middle paste.
// One trailing newline remains available for the next line being composed.
func compactPrompt(text string, selection [2]int) (string, [2]int) {
	lines := strings.Split(text, "\n")
	var result strings.Builder
	var mapped [2]int
	sourceOffset, outputOffset, lastContentEnd := 0, 0, 0
	for _, line := range lines {
		sourceLength := utf8.RuneCountInString(line)
		content := strings.TrimSuffix(line, "\r")
		keep := strings.TrimSpace(content) != ""
		if keep && result.Len() > 0 {
			result.WriteByte('\n')
			outputOffset++
		}
		start := outputOffset
		contentLength := 0
		if keep {
			result.WriteString(content)
			contentLength = utf8.RuneCountInString(content)
			outputOffset += contentLength
			lastContentEnd = sourceOffset + sourceLength
		}
		for i, position := range selection {
			if position >= sourceOffset && position <= sourceOffset+sourceLength {
				mapped[i] = start + min(position-sourceOffset, contentLength)
			}
		}
		sourceOffset += sourceLength + 1
	}
	if result.Len() > 0 && len(lines) > 1 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		result.WriteByte('\n')
		outputOffset++
		for i, position := range selection {
			if position > lastContentEnd {
				mapped[i] = outputOffset
			}
		}
	}
	return result.String(), mapped
}
