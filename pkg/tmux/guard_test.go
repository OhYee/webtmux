package tmux

import (
	"sync"
	"testing"
	"time"
)

// A normal first page load is bursty, but it must still remain below the
// configured per-second ceiling.
func TestGuardAllowsPartialWindowBelowCeiling(t *testing.T) {
	g := NewGuard(500)

	for i := 0; i < 500; i++ {
		if err := g.Record(); err != nil {
			t.Fatalf("tripped on a partial window after %d commands: %v", i, err)
		}
	}
	if tripped, _ := g.Tripped(); tripped {
		t.Error("guard tripped before the window had filled")
	}
}

func TestGuardTripsOnImmediateRate(t *testing.T) {
	g := NewGuard(5)

	for i := 0; i < 5; i++ {
		if err := g.Record(); err != nil {
			t.Fatalf("tripped at the configured ceiling after %d commands: %v", i, err)
		}
	}
	if err := g.Record(); err == nil {
		t.Fatal("guard did not trip when one second exceeded the ceiling")
	}
}

func TestGuardTripsOnSustainedRate(t *testing.T) {
	g := NewGuard(5)
	now := time.Now().Unix()
	for i := 1; i < guardBuckets; i++ {
		put(g, now-int64(i), 6)
	}
	// Keep the current second below the immediate ceiling so this exercises the
	// ten-second average rather than the new burst protection.
	put(g, now, 1)
	g.Record()

	if tripped, reason := g.Tripped(); !tripped {
		t.Fatal("guard did not trip on a sustained rate above the ceiling")
	} else if reason == "" {
		t.Error("a trip must say why")
	}
	if err := g.Record(); err == nil {
		t.Error("commands must be refused once tripped")
	}
}

// A busy patch inside an otherwise quiet window is normal as long as it stays
// within the configured per-second ceiling.
func TestGuardToleratesBursts(t *testing.T) {
	g := NewGuard(60)

	now := time.Now().Unix()
	for i := 0; i < guardBuckets; i++ {
		n := 2
		if i == 3 {
			n = 59 // one heavy second, still below the hard ceiling
		}
		seed(g, now-int64(guardBuckets-1-i), n)
	}

	if tripped, _ := g.Tripped(); tripped {
		t.Error("a single busy second below the ceiling must not trip")
	}
}

func TestGuardShutsDownWhatWeStarted(t *testing.T) {
	g := NewGuard(5)

	var wg sync.WaitGroup
	wg.Add(2)
	g.OnTrip(func() { wg.Done() })
	g.OnTrip(func() { wg.Done() })

	fill(g, 10, 100)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("registered shutdowns were not run on trip")
	}

	// Anything registered afterwards is already too late to keep running.
	late := make(chan struct{})
	g.OnTrip(func() { close(late) })
	select {
	case <-late:
	case <-time.After(2 * time.Second):
		t.Error("a shutdown registered after the trip must still run")
	}
}

func TestGuardUnregisterDoesNotRetainClosedResource(t *testing.T) {
	g := NewGuard(1)
	called := make(chan struct{}, 1)
	unregister := g.OnTrip(func() { called <- struct{}{} })
	unregister()

	if err := g.Record(); err != nil {
		t.Fatal(err)
	}
	if err := g.Record(); err == nil {
		t.Fatal("guard did not trip")
	}
	select {
	case <-called:
		t.Fatal("unregistered closer was called")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestGuardDisabledByZero(t *testing.T) {
	g := NewGuard(0)
	fill(g, 10, 500)
	if tripped, _ := g.Tripped(); tripped {
		t.Error("a zero limit must disable the check")
	}
}

// fill simulates n commands in each of the last `seconds` seconds.
func fill(g *Guard, seconds, n int) {
	now := time.Now().Unix()
	for s := 0; s < seconds; s++ {
		seed(g, now-int64(seconds-1-s), n)
	}
}

// seed writes a bucket directly, so tests do not have to run in real time.
func seed(g *Guard, sec int64, n int) {
	put(g, sec, n)
	g.Record()
}

func put(g *Guard, sec int64, n int) {
	g.mu.Lock()
	slot := int(sec % guardBuckets)
	g.at[slot], g.buckets[slot] = sec, n
	g.mu.Unlock()
}
