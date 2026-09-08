package hooks

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitTimeout(t *testing.T) {
	r := NewRegistry()
	var ran atomic.Bool
	r.On("slow", func(Event) {
		time.Sleep(150 * time.Millisecond)
		ran.Store(true)
	})
	r.FireAfter(Event{Name: "slow"})
	if r.WaitTimeout(10 * time.Millisecond) {
		t.Fatal("expected timeout while listener still running")
	}
	if !r.WaitTimeout(time.Second) {
		t.Fatal("expected wait to finish")
	}
	if !ran.Load() {
		t.Fatal("listener should have completed")
	}
}
