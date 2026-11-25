//go:build windows

package serialx

import (
	"reflect"

	"go.bug.st/serial"
	"golang.org/x/sys/windows"
)

func cancelIO(p serial.Port) {
	v := reflect.ValueOf(p)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}
	h := v.FieldByName("handle")
	if !h.IsValid() || (h.Kind() != reflect.Uintptr && h.Kind() != reflect.Uint64 && h.Kind() != reflect.Uint) {
		return
	}
	if handle := windows.Handle(h.Uint()); handle != 0 {
		_ = windows.CancelIoEx(handle, nil)
	}
}
