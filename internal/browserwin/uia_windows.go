//go:build windows && (amd64 || arm64)

package browserwin

import (
	"errors"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"uika-resonance/internal/wrt"
)

// Windows amd64 and arm64 VARIANT is 24 bytes, including the BRECORD union.
// The union is eight-byte aligned, including its pointer and integer members.
type uiaVariant struct {
	VT       uint16
	Reserved [3]uint16
	Data     uint64
	Tail     uint64
}

var (
	uiaBusy         atomic.Bool
	ole32           = syscall.NewLazyDLL("ole32.dll")
	oleaut32        = syscall.NewLazyDLL("oleaut32.dll")
	coInit          = ole32.NewProc("CoInitializeEx")
	coUninit        = ole32.NewProc("CoUninitialize")
	coCreate        = ole32.NewProc("CoCreateInstance")
	variantClear    = oleaut32.NewProc("VariantClear")
	allocString     = oleaut32.NewProc("SysAllocString")
	freeString      = oleaut32.NewProc("SysFreeString")
	stringLen       = oleaut32.NewProc("SysStringLen")
	automationCLSID = wrt.MustGUID("ff48dba4-60ef-4201-aa87-54103eef594e")
	automationIID   = wrt.MustGUID("30cbe57d-d9d0-452a-ab13-7ac5ac4825ee")
	automation2IID  = wrt.MustGUID("34723aff-0c9d-49d0-9896-7ab52df8cd8a")
)

func clearVariant(v *uiaVariant) { variantClear.Call(uintptr(unsafe.Pointer(v))) }

// Keep pointer arguments alive and off movable Go stacks through the COM call.
//
//go:uintptrescapes
func uiaCall(o *wrt.Object, slot int, args ...uintptr) (uintptr, error) {
	hr, err := wrt.VtCall(o, slot, args...)
	runtime.KeepAlive(o)
	return hr, err
}

// openUIA shares the one-in-flight guard and COM lifetime for bounded reads.
func openUIA(hwnd uintptr) (*wrt.Object, *wrt.Object, func(), error) {
	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	if !uiaBusy.CompareAndSwap(false, true) {
		return nil, nil, cleanup, errors.New("UIA busy")
	}
	cleanups = append(cleanups, func() { uiaBusy.Store(false) })
	runtime.LockOSThread()
	cleanups = append(cleanups, runtime.UnlockOSThread)
	hr, _, _ := coInit.Call(0, 0) // COINIT_MULTITHREADED
	if int32(hr) < 0 {
		return nil, nil, cleanup, errors.New("UIA COM initialization failed")
	}
	cleanups = append(cleanups, func() { coUninit.Call() })
	var p unsafe.Pointer
	hr, _, _ = coCreate.Call(uintptr(unsafe.Pointer(automationCLSID)), 0, 1, uintptr(unsafe.Pointer(automationIID)), uintptr(unsafe.Pointer(&p)))
	automation := wrt.NewObject(p)
	cleanups = append(cleanups, automation.Release)
	if int32(hr) < 0 || !automation.Valid() {
		return nil, nil, cleanup, errors.New("UIA activation failed")
	}
	var a2Ptr unsafe.Pointer
	_, qiErr := uiaCall(automation, 0, uintptr(unsafe.Pointer(automation2IID)), uintptr(unsafe.Pointer(&a2Ptr)))
	a2 := wrt.NewObject(a2Ptr)
	if qiErr == nil && a2.Valid() {
		_, _ = uiaCall(a2, 61, 1500)
		_, _ = uiaCall(a2, 63, 1500)
	}
	a2.Release()
	var rootPtr unsafe.Pointer
	_, err := uiaCall(automation, 6, hwnd, uintptr(unsafe.Pointer(&rootPtr)))
	root := wrt.NewObject(rootPtr)
	cleanups = append(cleanups, root.Release)
	if err != nil || !root.Valid() {
		return nil, nil, cleanup, errors.New("UIA window unavailable")
	}

	return automation, root, cleanup, nil
}

// ReadAddressBar reads only the matched window's unfocused omnibox. Callers
// must discard the raw URL after parsing; errors never contain its contents.
func ReadAddressBar(hwnd uintptr) (string, error) {
	automation, root, cleanup, err := openUIA(hwnd)
	defer cleanup()
	if err != nil {
		return "", err
	}

	// Each condition owns its own reference, including composite conditions.
	var conditions []*wrt.Object
	defer func() {
		for i := len(conditions) - 1; i >= 0; i-- {
			conditions[i].Release()
		}
	}()
	property := func(id uintptr, value *uiaVariant) (*wrt.Object, error) {
		// On Windows amd64 and arm64, a by-value 24-byte VARIANT is passed
		// as a pointer to a caller-owned copy.
		copyValue := *value
		var out unsafe.Pointer
		_, err := uiaCall(automation, 23, id, uintptr(unsafe.Pointer(&copyValue)), uintptr(unsafe.Pointer(&out)))
		o := wrt.NewObject(out)
		conditions = append(conditions, o)
		if err != nil || !o.Valid() {
			return nil, errors.New("UIA property condition failed")
		}
		return o, nil
	}
	textProperty := func(id uintptr, text string) (*wrt.Object, error) {
		utf, err := syscall.UTF16FromString(text)
		if err != nil {
			return nil, err
		}
		// VariantClear owns this BSTR after allocation.
		b, _, _ := allocString.Call(uintptr(unsafe.Pointer(&utf[0])))
		if b == 0 {
			return nil, errors.New("UIA BSTR allocation failed")
		}
		v := uiaVariant{VT: 8}
		v.Data = uint64(b)
		defer clearVariant(&v)
		return property(id, &v)
	}
	editValue := uiaVariant{VT: 3}
	editValue.Data = 50004
	edit, err := property(30003, &editValue)
	defer clearVariant(&editValue)
	if err != nil {
		return "", err
	}
	name, err := textProperty(30005, "Address and search bar")
	if err != nil {
		return "", err
	}
	aid, err := textProperty(30011, "view_1017")
	if err != nil {
		return "", err
	}
	combine := func(slot int, a, b *wrt.Object) (*wrt.Object, error) {
		var out unsafe.Pointer
		_, err := uiaCall(automation, slot, a.RawPtr(), b.RawPtr(), uintptr(unsafe.Pointer(&out)))
		o := wrt.NewObject(out)
		conditions = append(conditions, o)
		if err != nil || !o.Valid() {
			return nil, errors.New("UIA composite condition failed")
		}
		return o, nil
	}
	either, err := combine(28, name, aid)
	if err != nil {
		return "", err
	}
	selector, err := combine(25, edit, either)
	if err != nil {
		return "", err
	}
	var foundPtr unsafe.Pointer
	_, err = uiaCall(root, 5, 4, selector.RawPtr(), uintptr(unsafe.Pointer(&foundPtr)))
	found := wrt.NewObject(foundPtr)
	defer found.Release()
	if err != nil || !found.Valid() {
		return "", errors.New("UIA omnibox unavailable")
	}
	var focused int32
	if _, err = uiaCall(found, 26, uintptr(unsafe.Pointer(&focused))); err != nil || focused != 0 {
		return "", errors.New("UIA omnibox focused or unavailable")
	}
	var value uiaVariant
	defer clearVariant(&value)
	if _, err = uiaCall(found, 10, 30045, uintptr(unsafe.Pointer(&value))); err != nil {
		return "", err
	}
	if value.VT != 8 || value.Data == 0 {
		return "", errors.New("UIA omnibox value unavailable")
	}
	n, _, _ := stringLen.Call(uintptr(value.Data))
	if n > 8192 {
		return "", errors.New("UIA omnibox value too long")
	}
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(*(*unsafe.Pointer)(unsafe.Pointer(&value.Data))), int(n))), nil
}

// ReadTabNames reads at most limit tab names (capped at 200) from this window, never the desktop.
func ReadTabNames(hwnd uintptr, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	if limit > 200 {
		limit = 200
	}
	automation, root, cleanup, err := openUIA(hwnd)
	defer cleanup()
	if err != nil {
		return nil, err
	}
	value := uiaVariant{VT: 3, Data: 50019}
	var conditionPtr unsafe.Pointer
	_, err = uiaCall(automation, 23, 30003, uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&conditionPtr)))
	condition := wrt.NewObject(conditionPtr)
	defer condition.Release()
	if err != nil || !condition.Valid() {
		return nil, errors.New("UIA tab condition unavailable")
	}
	var arrayPtr unsafe.Pointer
	_, err = uiaCall(root, 6, 4, condition.RawPtr(), uintptr(unsafe.Pointer(&arrayPtr)))
	array := wrt.NewObject(arrayPtr)
	defer array.Release()
	if err != nil || !array.Valid() {
		return nil, errors.New("UIA tabs unavailable")
	}
	var count int32
	if _, err = uiaCall(array, 3, uintptr(unsafe.Pointer(&count))); err != nil || count < 0 {
		return nil, errors.New("UIA tab count unavailable")
	}
	if count > int32(limit) {
		count = int32(limit)
	}
	names := make([]string, 0, int(count))
	for i := int32(0); i < count; i++ {
		name, err := readTabName(array, i)
		if err != nil {
			// Preserve the attempted-name count for the caller's scan budget.
			return append(names, ""), err
		}
		names = append(names, name)
	}
	return names, nil
}

func readTabName(array *wrt.Object, index int32) (string, error) {
	var elemPtr unsafe.Pointer
	_, err := uiaCall(array, 4, uintptr(index), uintptr(unsafe.Pointer(&elemPtr)))
	elem := wrt.NewObject(elemPtr)
	defer elem.Release()
	if err != nil || !elem.Valid() {
		return "", errors.New("UIA tab unavailable")
	}
	var name unsafe.Pointer
	_, err = uiaCall(elem, 23, uintptr(unsafe.Pointer(&name)))
	if name != nil {
		defer freeString.Call(uintptr(name))
	}
	if err != nil {
		return "", errors.New("UIA tab name unavailable")
	}
	if name == nil {
		return "", nil
	}
	n, _, _ := stringLen.Call(uintptr(name))
	if n > 1024 {
		return "", nil
	}
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(name), int(n))), nil
}
