package common

import (
	"testing"
	"time"
)

func TestToTickClampsPreStartTimes(t *testing.T) {
	start := time.Unix(1000000000, 0)
	tk := NewTicker(start, 10*time.Second)

	if got := tk.ToTick(start.Add(-5 * time.Second)); got != 0 {
		t.Fatalf("pre-start time: got tick %d, want 0", got)
	}
	if got := tk.ToTick(start); got != 0 {
		t.Fatalf("start time: got tick %d, want 0", got)
	}
	// identity for the valid domain
	if got := tk.ToTick(start.Add(25 * time.Second)); got != 2 {
		t.Fatalf("post-start time: got tick %d, want 2", got)
	}
	if got := tk.ToTick(start.Add(10 * time.Second)); got != 1 {
		t.Fatalf("tick boundary: got tick %d, want 1", got)
	}
}

func TestNewTickerRejectsSubSecondInterval(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected NewTicker to panic on a sub-second interval")
		}
	}()
	NewTicker(time.Unix(1000000000, 0), 100*time.Millisecond)
}
