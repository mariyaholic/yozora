//go:build windows

package sysutil

import "testing"

func TestAcquireNamedMutexExclusive(t *testing.T) {
	name := `Local\yozora-test-singleton`
	ok1, release, err := AcquireNamedMutex(name)
	if err != nil || !ok1 {
		t.Fatalf("first acquire: ok=%v err=%v", ok1, err)
	}
	ok2, release2, err2 := AcquireNamedMutex(name)
	if err2 != nil {
		t.Fatalf("second acquire errored: %v", err2)
	}
	if ok2 {
		t.Fatal("second owner acquired an exclusive mutex")
	}
	release2()
	release()
	ok3, release3, err3 := AcquireNamedMutex(name)
	if err3 != nil || !ok3 {
		t.Fatalf("reacquire after release: ok=%v err=%v", ok3, err3)
	}
	release3()
}
