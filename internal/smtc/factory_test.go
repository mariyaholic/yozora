//go:build windows

package smtc

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"uika-resonance/internal/wrt"
)

func TestFactoryCacheReusesSuccessfulLookup(t *testing.T) {
	var cache factoryCache
	want := wrt.NewObject(nil)
	calls := 0
	load := func() (*wrt.Object, error) { calls++; return want, nil }

	first, err := cache.get(load)
	if err != nil || first != want {
		t.Fatalf("first get = (%v, %v), want (%v, nil)", first, err, want)
	}
	second, err := cache.get(load)
	if err != nil || second != first {
		t.Fatalf("second get = (%v, %v), want the cached %v", second, err, first)
	}
	if calls != 1 {
		t.Fatalf("factory resolved %d times, want exactly once", calls)
	}
}

func TestFactoryCacheRetriesAfterFailure(t *testing.T) {
	var cache factoryCache
	sentinel := errors.New("activation failed")
	attempts := 0
	load := func() (*wrt.Object, error) {
		attempts++
		if attempts == 1 {
			return nil, sentinel
		}
		return wrt.NewObject(nil), nil
	}

	if _, err := cache.get(load); !errors.Is(err, sentinel) {
		t.Fatalf("first get error = %v, want %v", err, sentinel)
	}
	if _, err := cache.get(load); err != nil {
		t.Fatalf("second get = %v, want a retry to succeed", err)
	}
	if attempts != 2 {
		t.Fatalf("failed lookup attempted %d times, want a retry", attempts)
	}
}

func TestFactoryCacheConcurrentGetResolvesOnce(t *testing.T) {
	var cache factoryCache
	want := wrt.NewObject(nil)
	var calls atomic.Int64
	load := func() (*wrt.Object, error) { calls.Add(1); return want, nil }

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := cache.get(load); err != nil || got != want {
				t.Errorf("concurrent get = (%v, %v), want (%v, nil)", got, err, want)
			}
		}()
	}
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatalf("factory resolved %d times under concurrency, want once", calls.Load())
	}
}
