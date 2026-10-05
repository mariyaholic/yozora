//go:build windows

package smtc

import (
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"unsafe"

	"uika-resonance/internal/wrt"
)

// Synthetic COM objects exercise the actual raw-vtable capture/read path.
// They contain no real media metadata and never perform network access.
type thumbCOMFixture struct{ vt *[32]uintptr }

func newThumbCOMFixture() *thumbCOMFixture {
	f := &thumbCOMFixture{vt: new([32]uintptr)}
	fail := syscall.NewCallback(func(uintptr, uintptr) uintptr { return 0x80004005 })
	for i := range f.vt {
		f.vt[i] = fail
	}
	f.vt[2] = syscall.NewCallback(func(uintptr) uintptr { return 1 })
	return f
}
func (f *thumbCOMFixture) object() *wrt.Object { return wrt.NewObject(unsafe.Pointer(f)) }

// COM callback arguments are native addresses, not uintptrs manufactured by
// Go pointer arithmetic. Scope the checkptr exception to this ABI boundary.
//
//go:nocheckptr
func fixtureOut(out uintptr) unsafe.Pointer { return unsafe.Pointer(out) }

func fixtureObjectGetter(f *thumbCOMFixture, slot int, target *thumbCOMFixture) {
	f.vt[slot] = syscall.NewCallback(func(_ uintptr, out uintptr) uintptr {
		*(*unsafe.Pointer)(fixtureOut(out)) = unsafe.Pointer(target)
		return 0
	})
}
func TestCapturedThumbnailReusesFirstResult(t *testing.T) {
	sess, op, mp, ref := newThumbCOMFixture(), newThumbCOMFixture(), newThumbCOMFixture(), newThumbCOMFixture()
	fixtureObjectGetter(sess, 7, op)
	op.vt[0] = syscall.NewCallback(func(_ uintptr, _ uintptr, out uintptr) uintptr {
		*(*unsafe.Pointer)(fixtureOut(out)) = unsafe.Pointer(op)
		return 0
	})
	op.vt[7] = syscall.NewCallback(func(_ uintptr, out uintptr) uintptr { *(*int32)(fixtureOut(out)) = 1; return 0 })
	fixtureObjectGetter(op, 8, mp)
	fixtureObjectGetter(mp, 15, ref)
	var reads atomic.Int32
	ref.vt[6] = syscall.NewCallback(func(_ uintptr, _ uintptr) uintptr { reads.Add(1); return 0x80004005 })
	// Metadata getters fail harmlessly; the thumbnail alone is under test.
	sess.vt[6] = syscall.NewCallback(func(_ uintptr, out uintptr) uintptr { *(*uintptr)(fixtureOut(out)) = 0; return 0 })
	// Warm the callback stack before passing stack-backed COM out parameters.
	var warm uintptr
	syscall.SyscallN(sess.vt[6], uintptr(unsafe.Pointer(sess)), uintptr(unsafe.Pointer(&warm)))
	tr, err := (&Manager{}).session(sess.object())
	if err != nil || tr.Thumb == nil {
		t.Fatalf("capture: %v", err)
	}
	_, first := tr.Thumb()
	if first == nil || !strings.Contains(first.Error(), "smtc thumb: open") {
		t.Fatalf("first read: %v", first)
	}
	_, second := tr.Thumb()
	if second == nil || second.Error() != first.Error() {
		t.Fatalf("reuse lost first result: first=%v second=%v", first, second)
	}
	if reads.Load() != 1 {
		t.Fatalf("stream opened %d times", reads.Load())
	}
	runtime.KeepAlive([]*thumbCOMFixture{sess, op, mp, ref})
}
