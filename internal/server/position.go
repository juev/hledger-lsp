package server

import "unicode/utf8"

func utf16Len(s string) int {
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

func utf16OffsetToByteOffset(s string, utf16Offset int) int {
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
