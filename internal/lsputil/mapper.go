// Package lsputil provides UTF-8/UTF-16 position mapping utilities for LSP.
//
// LSP uses UTF-16 code units for character positions, while Go uses UTF-8.
// This package handles conversion automatically, including proper handling
// of multi-byte UTF-8 sequences and UTF-16 surrogate pairs (e.g., emoji).
//
// PositionMapper is the main type that performs bidirectional conversion
// and applies LSP text changes. It handles boundary conditions safely:
//   - Out-of-bounds positions are clamped to valid ranges
//   - Invalid ranges (start > end) are swapped automatically
package lsputil

import (
	"sort"
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"
)

type PositionMapper struct {
	content    string
	lines      []string
	lineStarts []int
}

func NewPositionMapper(content string) *PositionMapper {
	m := &PositionMapper{content: content}
	m.lines = strings.Split(content, "\n")
	m.lineStarts = make([]int, len(m.lines))

	offset := 0
	for i, line := range m.lines {
		m.lineStarts[i] = offset
		offset += len(line) + 1
	}
	return m
}

func (m *PositionMapper) LSPToByte(pos protocol.Position) int {
	line := int(pos.Line)
	if line >= len(m.lines) {
		return len(m.content)
	}
	byteOffset := m.lineStarts[line]
	byteOffset += UTF16OffsetToByteOffset(m.lines[line], int(pos.Character))
	return byteOffset
}

func (m *PositionMapper) ByteToLSP(byteOffset int) protocol.Position {
	if byteOffset <= 0 {
		return protocol.Position{Line: 0, Character: 0}
	}
	if byteOffset >= len(m.content) {
		lastLine := len(m.lines) - 1
		if lastLine < 0 {
			return protocol.Position{Line: 0, Character: 0}
		}
		return protocol.Position{
			Line:      uint32(lastLine),
			Character: uint32(UTF16Len(m.lines[lastLine])),
		}
	}

	line := sort.Search(len(m.lineStarts), func(i int) bool {
		return m.lineStarts[i] > byteOffset
	}) - 1

	if line < 0 {
		line = 0
	}

	lineByteOffset := byteOffset - m.lineStarts[line]
	utf16Char := ByteOffsetToUTF16(m.lines[line], lineByteOffset)

	return protocol.Position{
		Line:      uint32(line),
		Character: uint32(utf16Char),
	}
}

func (m *PositionMapper) LineUTF16Len(line int) int {
	if line < 0 || line >= len(m.lines) {
		return 0
	}
	return UTF16Len(m.lines[line])
}

func (m *PositionMapper) LineRuneLen(line int) int {
	if line < 0 || line >= len(m.lines) {
		return 0
	}
	return utf8.RuneCountInString(m.lines[line])
}

func (m *PositionMapper) ApplyChange(r protocol.Range, text string) string {
	startByte := m.LSPToByte(r.Start)
	endByte := m.LSPToByte(r.End)

	if startByte > endByte {
		startByte, endByte = endByte, startByte
	}

	if startByte > len(m.content) {
		startByte = len(m.content)
	}
	if endByte > len(m.content) {
		endByte = len(m.content)
	}

	return m.content[:startByte] + text + m.content[endByte:]
}

func UTF16Len(s string) int {
	count := 0
	for _, r := range s {
		if r >= 0x10000 {
			count += 2
		} else {
			count++
		}
	}
	return count
}

func UTF16OffsetToByteOffset(s string, utf16Offset int) int {
	byteOffset := 0
	utf16Count := 0
	for _, r := range s {
		if utf16Count >= utf16Offset {
			break
		}
		byteOffset += utf8.RuneLen(r)
		if r >= 0x10000 {
			utf16Count += 2
		} else {
			utf16Count++
		}
	}
	return byteOffset
}

func ByteOffsetToUTF16(s string, byteOffset int) int {
	utf16Count := 0
	currentByte := 0
	for _, r := range s {
		if currentByte >= byteOffset {
			break
		}
		currentByte += utf8.RuneLen(r)
		if r >= 0x10000 {
			utf16Count += 2
		} else {
			utf16Count++
		}
	}
	return utf16Count
}

func RuneCount(s string) int {
	return utf8.RuneCountInString(s)
}
