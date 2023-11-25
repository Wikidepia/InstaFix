package utils

import "unsafe"

func S2B(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func B2S(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}
