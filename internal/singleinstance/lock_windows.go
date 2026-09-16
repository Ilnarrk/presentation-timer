//go:build windows

package singleinstance

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var createMutexW = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateMutexW")

type Lock struct {
	handle windows.Handle
}

func Acquire() (*Lock, error) {
	name, err := windows.UTF16PtrFromString(`Local\PresentationTimer.SingleInstance`)
	if err != nil {
		return nil, err
	}
	handle, _, callErr := createMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, callErr
	}
	if errors.Is(callErr, syscall.Errno(183)) { // ERROR_ALREADY_EXISTS
		_ = windows.CloseHandle(windows.Handle(handle))
		return nil, ErrAlreadyRunning
	}
	return &Lock{handle: windows.Handle(handle)}, nil
}

func (lock *Lock) Close() error {
	if lock == nil || lock.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(lock.handle)
	lock.handle = 0
	return err
}
