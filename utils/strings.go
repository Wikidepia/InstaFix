package utils

import "unicode/utf8"

// Substr returns a substring of a given string, starting at the specified index
// and with a specified length.
// It handles UTF-8 encoded strings.
// Taken from https://github.com/goravel/framework/
func Substr(str string, start int, length ...int) string {
	// Convert the string to a rune slice for proper handling of UTF-8 encoding.
	runes := []rune(str)
	strLen := utf8.RuneCountInString(str)
	end := strLen
	// Check if the start index is out of bounds.
	if start >= strLen {
		return ""
	}

	// If the start index is negative, count backwards from the end of the string.
	if start < 0 {
		start = strLen + start
		if start < 0 {
			start = 0
		}
	}

	if len(length) > 0 {
		if length[0] >= 0 {
			end = start + length[0]
		} else {
			end = strLen + length[0]
		}
	}

	// If the length is 0, return the substring from start to the end of the string.
	if len(length) == 0 {
		return string(runes[start:])
	}

	// Handle the case where lenArg is negative and less than start
	if end < start {
		return ""
	}

	if end > strLen {
		end = strLen
	}

	// Return the substring.
	return string(runes[start:end])
}
