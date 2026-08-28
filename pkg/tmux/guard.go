package tmux

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// guardBuckets is how many one-second slots the rate is averaged over.
const guardBuckets = 10

// Guard stops this server from being the reason tmux falls over.
//
// tmux is single threaded: every command it services is time it is not
// redrawing anyone's terminal. This program has already taken the user's tmux
// down three times by asking too often — first by forking a process per query,
// then by polling paths nobody had counted. Each of those was found afterwards,
// from the wreckage.
//
// So the rate is measured rather than assumed. Past either the one-second or
// sustained ceiling the guard trips: it shuts down everything this process
// started and refuses to issue another command. Losing the web UI is a bad
// outcome; taking the user's sessions with it is a much worse one, and the
// guard exists to make sure the second cannot happen quietly.
type Guard struct {
	// limit is the immediate and sustained commands-per-second ceiling. Zero disables.
	limit int

	mu      sync.Mutex
	buckets [guardBuckets]int
	at      [guardBuckets]int64 // unix second owning each bucket
	tripped bool
	reason  string
	closers map[uint64]func()
	nextID  uint64
	recent  []time.Time
}

// NewGuard creates a rate guard. A limit of zero disables tripping, which is
// only sensible when something else is enforcing the ceiling.
func NewGuard(limit int) *Guard {
	return &Guard{limit: limit, closers: make(map[uint64]func())}
}

// OnTrip registers something to shut down when the guard trips. Every process
// this server starts should be registered, since the whole response is to stop
// touching tmux and let go of what we are holding.
func (g *Guard) OnTrip(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	g.mu.Lock()

	if g.tripped {
		g.mu.Unlock()
		// Already stopped: shut this one down rather than leave it running.
		go fn()
		return func() {}
	}
	g.nextID++
	id := g.nextID
	g.closers[id] = fn
	g.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			delete(g.closers, id)
			g.mu.Unlock()
		})
	}
}

// Record accounts for one tmux command and reports whether it may proceed.
func (g *Guard) Record() error {
	if g == nil {
		return nil
	}

	g.mu.Lock()

	if g.tripped {
		reason := g.reason
		g.mu.Unlock()
		return fmt.Errorf("tmux access halted: %s", reason)
	}

	nowTime := time.Now()
	now := nowTime.Unix()
	slot := int(now % guardBuckets)
	if g.at[slot] != now {
		// Reuse of a slot from a previous window: it is a new second.
		g.at[slot], g.buckets[slot] = now, 0
	}
	g.buckets[slot]++
	cutoff := nowTime.Add(-time.Second)
	first := 0
	for first < len(g.recent) && !g.recent[first].After(cutoff) {
		first++
	}
	if first > 0 {
		copy(g.recent, g.recent[first:])
		g.recent = g.recent[:len(g.recent)-first]
	}
	g.recent = append(g.recent, nowTime)

	if g.limit <= 0 {
		g.mu.Unlock()
		return nil
	}

	var reason string
	if len(g.recent) > g.limit {
		reason = fmt.Sprintf("%d tmux commands in one second, above the %d/s ceiling",
			len(g.recent), g.limit)
	}

	// Only count seconds inside the window, so a quiet start is not averaged
	// in as zero and made to look safe. The immediate ceiling above still
	// applies while this longer window is filling: tmux can be overwhelmed
	// before ten seconds have elapsed.
	total, live := 0, 0
	for i := range g.buckets {
		if now-g.at[i] < guardBuckets {
			total += g.buckets[i]
			live++
		}
	}
	if reason == "" && live == guardBuckets && float64(total)/float64(live) > float64(g.limit) {
		rate := float64(total) / float64(live)
		reason = fmt.Sprintf("%.0f tmux commands/second sustained over %ds, above the %.0f/s ceiling",
			rate, guardBuckets, float64(g.limit))
	}
	if reason == "" {
		g.mu.Unlock()
		return nil
	}

	g.tripped = true
	g.reason = reason
	reason, registered := g.reason, g.closers
	g.closers = make(map[uint64]func())
	g.mu.Unlock()

	log.Printf("SAFETY: %s", reason)
	log.Printf("SAFETY: shutting down everything this server started and halting tmux access.")
	log.Printf("SAFETY: restart webtmux once you know why the rate climbed; raise --max-tmux-rate only if you are sure it is legitimate.")

	for _, fn := range registered {
		go fn()
	}

	return fmt.Errorf("tmux access halted: %s", reason)
}

// Tripped reports whether tmux access has been halted, and why.
func (g *Guard) Tripped() (bool, string) {
	if g == nil {
		return false, ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tripped, g.reason
}

// Rate returns the current commands-per-second average, for reporting.
func (g *Guard) Rate() float64 {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now().Unix()
	total, live := 0, 0
	for i := range g.buckets {
		if now-g.at[i] < guardBuckets {
			total += g.buckets[i]
			live++
		}
	}
	if live == 0 {
		return 0
	}
	return float64(total) / float64(live)
}
