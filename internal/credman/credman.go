//go:build windows

package credman

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const credTypeGeneric = 1

type credBlob struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
	CredentialBlobSize uint32
	CredentialBlob     uintptr
}

var (
	modAdvapi32    = syscall.NewLazyDLL("advapi32.dll")
	procCredWrite  = modAdvapi32.NewProc("CredWriteW")
	procCredRead   = modAdvapi32.NewProc("CredReadW")
	procCredDelete = modAdvapi32.NewProc("CredDeleteW")
	procCredFree   = modAdvapi32.NewProc("CredFree")
)

func targetName(key string) string { return "uika-resonance:" + key }

func Set(key, secret string) error {
	target, err := syscall.UTF16PtrFromString(targetName(key))
	if err != nil {
		return err
	}
	blob := []uint16{}
	for _, r := range secret {
		blob = append(blob, uint16(r))
	}
	c := credBlob{
		Flags:              0,
		Type:               credTypeGeneric,
		TargetName:         target,
		Comment:            nil,
		Persist:            2,
		CredentialBlobSize: uint32(len(blob) * 2),
		CredentialBlob:     uintptr(unsafe.Pointer(&blob[0])),
	}
	r1, _, _ := procCredWrite.Call(uintptr(unsafe.Pointer(&c)), 0)
	if r1 == 0 {
		return fmt.Errorf("credman: CredWriteW failed: %v", syscall.GetLastError())
	}
	return nil
}

func Get(key string) (string, error) {
	target, err := syscall.UTF16PtrFromString(targetName(key))
	if err != nil {
		return "", err
	}
	var p uintptr
	r1, _, _ := procCredRead.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&p)))
	if r1 == 0 {
		return "", errors.New("credman: not found")
	}
	defer procCredFree.Call(p)
	c := (*credBlob)(unsafe.Pointer(p))
	if c.CredentialBlobSize == 0 {
		return "", errors.New("credman: empty secret")
	}
	b := unsafe.Slice((*uint16)(unsafe.Pointer(c.CredentialBlob)), c.CredentialBlobSize/2)
	runes := make([]rune, len(b))
	for i, v := range b {
		runes[i] = rune(v)
	}
	return string(runes), nil
}

func Delete(key string) error {
	target, err := syscall.UTF16PtrFromString(targetName(key))
	if err != nil {
		return err
	}
	r1, _, _ := procCredDelete.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r1 == 0 {
		return fmt.Errorf("credman: CredDeleteW failed: %v", syscall.GetLastError())
	}
	return nil
}
