package ui

import "strings"

type diffKind int

const (
	unchangedLine diffKind = iota
	addedLine
	removedLine
	// skippedLines stands for unchanged lines left out between hunks.
	skippedLines
)

type diffLine struct {
	kind diffKind
	text string
}

// lineDiff compares two texts line by line through their longest common subsequence,
// listing removed lines before the added ones that replace them, as git does.
// Context files are small, so the quadratic table is fine.
func lineDiff(before string, after string) []diffLine {
	beforeLines := splitLines(before)
	afterLines := splitLines(after)
	common := make([][]int, len(beforeLines)+1)
	for index := range common {
		common[index] = make([]int, len(afterLines)+1)
	}
	for beforeIndex := len(beforeLines) - 1; beforeIndex >= 0; beforeIndex-- {
		for afterIndex := len(afterLines) - 1; afterIndex >= 0; afterIndex-- {
			if beforeLines[beforeIndex] == afterLines[afterIndex] {
				common[beforeIndex][afterIndex] = common[beforeIndex+1][afterIndex+1] + 1
			} else {
				common[beforeIndex][afterIndex] = max(common[beforeIndex+1][afterIndex], common[beforeIndex][afterIndex+1])
			}
		}
	}
	var lines []diffLine
	beforeIndex, afterIndex := 0, 0
	for beforeIndex < len(beforeLines) || afterIndex < len(afterLines) {
		switch {
		case beforeIndex < len(beforeLines) && afterIndex < len(afterLines) && beforeLines[beforeIndex] == afterLines[afterIndex]:
			lines = append(lines, diffLine{kind: unchangedLine, text: beforeLines[beforeIndex]})
			beforeIndex++
			afterIndex++
		case beforeIndex < len(beforeLines) && (afterIndex == len(afterLines) || common[beforeIndex+1][afterIndex] >= common[beforeIndex][afterIndex+1]):
			lines = append(lines, diffLine{kind: removedLine, text: beforeLines[beforeIndex]})
			beforeIndex++
		default:
			lines = append(lines, diffLine{kind: addedLine, text: afterLines[afterIndex]})
			afterIndex++
		}
	}
	return lines
}

// diffHunks keeps the changed lines with a few unchanged ones around them, like a unified diff.
func diffHunks(lines []diffLine, contextLines int) []diffLine {
	keep := make([]bool, len(lines))
	for index, line := range lines {
		if line.kind == unchangedLine {
			continue
		}
		for nearby := max(index-contextLines, 0); nearby <= min(index+contextLines, len(lines)-1); nearby++ {
			keep[nearby] = true
		}
	}
	var hunks []diffLine
	for index, line := range lines {
		if keep[index] {
			hunks = append(hunks, line)
		} else if len(hunks) == 0 || hunks[len(hunks)-1].kind != skippedLines {
			hunks = append(hunks, diffLine{kind: skippedLines})
		}
	}
	return hunks
}

// diffCounts counts added and removed lines.
func diffCounts(lines []diffLine) (added int, removed int) {
	for _, line := range lines {
		switch line.kind {
		case addedLine:
			added++
		case removedLine:
			removed++
		}
	}
	return added, removed
}

func splitLines(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
