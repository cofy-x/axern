//go:build linux

package enforcement

import "unsafe"

func unsafePointer[T any](value *T) unsafe.Pointer { return unsafe.Pointer(value) }
