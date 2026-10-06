//go:build windows

package wrt

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	modCombase                    = syscall.NewLazyDLL("combase.dll")
	procRoGetActivationFactory    = modCombase.NewProc("RoGetActivationFactory")
	procWindowsCreateString       = modCombase.NewProc("WindowsCreateString")
	procWindowsDeleteString       = modCombase.NewProc("WindowsDeleteString")
	procWindowsGetStringRawBuffer = modCombase.NewProc("WindowsGetStringRawBuffer")
	modOle32                      = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx            = modOle32.NewProc("CoInitializeEx")
)

var (
	IID_IActivationFactory = MustGUID("00000035-0000-0000-C000-000000000046")
	IID_IInspectable       = MustGUID("af86e2e0-b12d-4c6a-9c5a-d7aa65101e90")
	IID_IAsyncInfo         = MustGUID("00000036-0000-0000-C000-000000000046")

	BaseIAsyncOperation = "9fc2b0bb-e446-44e2-aa61-9cab8f636af2"
	BaseIVectorView     = "bbe1fa4c-b0e3-4583-baef-1f1b2e483e56"
)

func SigInterface(iid string) string { return "{" + iid + "}" }

func SigU32() string { return "u4" }

func SigClass(fullName, defaultIID string) string {
	return fmt.Sprintf("rc(%s;{%s})", fullName, defaultIID)
}

func AsyncOperationIID(argSig string) *GUID {
	return ParameterizedGUID(BaseIAsyncOperation, argSig)
}

func VectorViewIID(argSig string) *GUID {
	return ParameterizedGUID(BaseIVectorView, argSig)
}

func NewHString(s string) (uintptr, error) {
	u16, err := syscall.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	n := len(u16) - 1
	var h uintptr
	r1, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u16[0])), uintptr(n), uintptr(unsafe.Pointer(&h)))
	if r1 != 0 {
		return 0, fmt.Errorf("wrt: WindowsCreateString failed: 0x%08x", uint32(r1))
	}
	return h, nil
}

func FreeHString(h uintptr) {
	if h != 0 {
		procWindowsDeleteString.Call(h)
	}
}

func HStringToString(h uintptr) string {
	if h == 0 {
		return ""
	}
	var n uint32
	buf, _, _ := procWindowsGetStringRawBuffer.Call(h, uintptr(unsafe.Pointer(&n)))
	if buf == 0 || n == 0 {
		return ""
	}
	s16 := unsafe.Slice((*uint16)(unsafe.Pointer(buf)), n)
	return syscall.UTF16ToString(s16)
}

type Object struct{ p unsafe.Pointer }

func NewObject(raw unsafe.Pointer) *Object { return &Object{p: raw} }

func (o *Object) Raw() unsafe.Pointer { return o.p }
func (o *Object) RawPtr() uintptr     { return uintptr(o.p) }
func (o *Object) Valid() bool         { return o != nil && o.p != nil }

func (o *Object) Release() {
	if o != nil && o.p != nil {
		vt := vtableSlot(o.p, 2)
		syscall.Syscall6(vt, 1, o.RawPtr(), 0, 0, 0, 0, 0)
		o.p = nil
	}
}

func (o *Object) QueryInterface(iid *GUID) (*Object, error) {
	if !o.Valid() {
		return nil, errors.New("wrt: QueryInterface on nil object")
	}
	var out unsafe.Pointer
	hr, _, _ := syscall.Syscall6(vtableSlot(o.p, 0), 3,
		o.RawPtr(), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)), 0, 0, 0)
	if hr != 0 {
		return nil, HRESULTError("QueryInterface", hr)
	}
	if out == nil {
		return nil, fmt.Errorf("wrt: QueryInterface returned null for %s", iid)
	}
	return &Object{out}, nil
}

func QueryInterface(o *Object, iid *GUID) (*Object, error) { return o.QueryInterface(iid) }

func vtableSlot(p unsafe.Pointer, slot int) uintptr {
	vt := *(*unsafe.Pointer)(p)
	return *(*uintptr)(unsafe.Pointer(uintptr(vt) + uintptr(slot)*unsafe.Sizeof(uintptr(0))))
}

func VtableSlot(o *Object, slot int) uintptr { return vtableSlot(o.p, slot) }

func call0(fn, a uintptr) uintptr    { r, _, _ := syscall.Syscall6(fn, 1, a, 0, 0, 0, 0, 0); return r }
func call1(fn, a, b uintptr) uintptr { r, _, _ := syscall.Syscall6(fn, 2, a, b, 0, 0, 0, 0); return r }
func call2(fn, a, b, c uintptr) uintptr {
	r, _, _ := syscall.Syscall6(fn, 3, a, b, c, 0, 0, 0)
	return r
}
func call3(fn, a, b, c, d uintptr) uintptr {
	r, _, _ := syscall.Syscall6(fn, 4, a, b, c, d, 0, 0)
	return r
}
func call4(fn, a, b, c, d, e uintptr) uintptr {
	r, _, _ := syscall.Syscall6(fn, 5, a, b, c, d, e, 0)
	return r
}
func call5(fn, a, b, c, d, e, f uintptr) uintptr {
	r, _, _ := syscall.Syscall6(fn, 6, a, b, c, d, e, f)
	return r
}

func HRESULTError(what string, hr uintptr) error {
	return fmt.Errorf("wrt: %s failed: 0x%08x", what, uint32(int32(hr)))
}

func VtCall(o *Object, slot int, args ...uintptr) (uintptr, error) {
	if !o.Valid() {
		return 0, errors.New("wrt: call on nil object")
	}
	fn := vtableSlot(o.p, slot)

	full := append([]uintptr{o.RawPtr()}, args...)
	var r uintptr
	switch len(full) {
	case 1:
		r = call0(fn, full[0])
	case 2:
		r = call1(fn, full[0], full[1])
	case 3:
		r = call2(fn, full[0], full[1], full[2])
	case 4:
		r = call3(fn, full[0], full[1], full[2], full[3])
	case 5:
		r = call4(fn, full[0], full[1], full[2], full[3], full[4])
	case 6:
		r = call5(fn, full[0], full[1], full[2], full[3], full[4], full[5])
	default:
		return 0, errors.New("wrt: too many vtable args")
	}
	if int32(r) < 0 {
		return r, HRESULTError(fmt.Sprintf("vtable slot %d", slot), r)
	}
	return r, nil
}

func ComInit() error {
	r1, _, _ := procCoInitializeEx.Call(0, 0)
	switch uint32(r1) {
	case 0, 1:
		return nil
	case 0x80010106:
		return nil
	default:
		return fmt.Errorf("wrt: CoInitializeEx failed: 0x%08x", uint32(r1))
	}
}

func GetActivationFactory(className string, iid *GUID) (*Object, error) {
	h, err := NewHString(className)
	if err != nil {
		return nil, err
	}
	defer FreeHString(h)
	var out unsafe.Pointer
	r1, _, _ := procRoGetActivationFactory.Call(h, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if r1 != 0 {
		return nil, HRESULTError("RoGetActivationFactory("+className+")", r1)
	}
	if out == nil {
		return nil, fmt.Errorf("wrt: RoGetActivationFactory(%s) returned null", className)
	}
	return &Object{out}, nil
}

func ActivateInstance(className string) (*Object, error) {
	f, err := GetActivationFactory(className, IID_IActivationFactory)
	if err != nil {
		return nil, err
	}
	defer f.Release()
	var out unsafe.Pointer
	if _, err := VtCall(f, 6, uintptr(unsafe.Pointer(&out))); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("wrt: ActivateInstance(%s) returned null", className)
	}
	return &Object{out}, nil
}

const (
	asyncStarted   = 0
	asyncCompleted = 1
	asyncCanceled  = 2
	asyncError     = 3
)

func Await(op *Object, timeout time.Duration) error {
	info, err := op.QueryInterface(IID_IAsyncInfo)
	if err != nil {
		return err
	}
	defer info.Release()
	deadline := time.Now().Add(timeout)
	for {
		var st int32
		if _, err := VtCall(info, 7, uintptr(unsafe.Pointer(&st))); err != nil {
			return err
		}
		switch st {
		case asyncCompleted:
			return nil
		case asyncCanceled:
			return errors.New("wrt: async operation canceled")
		case asyncError:
			var code int32
			if _, err := VtCall(info, 8, uintptr(unsafe.Pointer(&code))); err == nil {
				return fmt.Errorf("wrt: async operation error: 0x%08x", uint32(code))
			}
			return errors.New("wrt: async operation error")
		}
		if time.Now().After(deadline) {
			_, _ = VtCall(info, 9)
			return errors.New("wrt: async operation timed out")
		}
		time.Sleep(4 * time.Millisecond)
	}
}

func GetResults(op *Object, opIID *GUID, out unsafe.Pointer) error {
	typed, err := op.QueryInterface(opIID)
	if err != nil {
		return err
	}
	defer typed.Release()
	_, err = VtCall(typed, 8, uintptr(out))
	return err
}

func CallStringGetter(o *Object, slot int) (string, error) {
	var h uintptr
	if _, err := VtCall(o, slot, uintptr(unsafe.Pointer(&h))); err != nil {
		return "", err
	}
	defer FreeHString(h)
	return HStringToString(h), nil
}

func CallI64Getter(o *Object, slot int) (int64, error) {
	var v int64
	if _, err := VtCall(o, slot, uintptr(unsafe.Pointer(&v))); err != nil {
		return 0, err
	}
	return v, nil
}

func CallU32Getter(o *Object, slot int) (uint32, error) {
	var v uint32
	if _, err := VtCall(o, slot, uintptr(unsafe.Pointer(&v))); err != nil {
		return 0, err
	}
	return v, nil
}

func CallI32Getter(o *Object, slot int) (int32, error) {
	var v int32
	if _, err := VtCall(o, slot, uintptr(unsafe.Pointer(&v))); err != nil {
		return 0, err
	}
	return v, nil
}

func CallObjectGetter(o *Object, slot int) (*Object, error) {
	var p unsafe.Pointer
	if _, err := VtCall(o, slot, uintptr(unsafe.Pointer(&p))); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("wrt: object getter returned null")
	}
	return &Object{p}, nil
}

func CallObjectMethod(o *Object, slot int) (*Object, error) { return CallObjectGetter(o, slot) }

func CallU64Getter(o *Object, slot int) (uint64, error) {
	var v uint64
	if _, err := VtCall(o, slot, uintptr(unsafe.Pointer(&v))); err != nil {
		return 0, err
	}
	return v, nil
}

func CallAsync(o *Object, slot int, timeout time.Duration) (*Object, error) {
	op, err := CallObjectGetter(o, slot)
	if err != nil {
		return nil, err
	}
	if err := Await(op, timeout); err != nil {
		op.Release()
		return nil, err
	}
	return op, nil
}

func CallAsyncObj(o *Object, slot int, arg uintptr, timeout time.Duration) (*Object, error) {
	var p unsafe.Pointer
	if _, err := VtCall(o, slot, arg, uintptr(unsafe.Pointer(&p))); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("wrt: async method returned null")
	}
	op := &Object{p}
	if err := Await(op, timeout); err != nil {
		op.Release()
		return nil, err
	}
	return op, nil
}

var _ = runtime.LockOSThread
