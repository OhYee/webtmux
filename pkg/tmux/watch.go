package tmux

import (
	"encoding/json"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// How many trailing lines of a pane to show on a card.
const tailLines = 4

// A board refresh must have bounded tmux cost regardless of how many panes
// exist. Metadata is one list-panes command; previews are filled progressively.
const maxCapturesPerWatch = 8

// How long a skipped pane may go without being read anyway.
const recaptureAfter = 30 * time.Second

// Viewers polling at about the same time share one build instead of repeating
// the same metadata query and preview captures.
const watchSnapshotTTL = 3 * time.Second

const readerSnapshotTTL = 3 * time.Second

// paneActivity caches board and reader captures between polls.
type paneActivity struct {
	// tail and activity let a poll skip capture-pane entirely. tmux stamps a
	// window whenever it produces output, so an unchanged stamp means nothing
	// in that window can have printed and the cached tail is still current.
	tail     []string
	activity time.Time
	captured time.Time
	// The reader's capture is a different, far more expensive read than the
	// board's, so its fingerprint is tracked separately.
	readDigest   string
	readActivity time.Time
}

// WatchTracker holds capture caches shared by every viewer.
type WatchTracker struct {
	mu            sync.Mutex
	panes         map[string]paneActivity
	captureCursor int
	// serverPID fingerprints the tmux server the pane history belongs to.
	serverPID int

	snapshotMu    sync.Mutex
	snapshotBoard *Watch
	snapshotAt    time.Time

	layoutMu  sync.Mutex
	layoutRaw string
	layoutAt  time.Time

	readerMu    sync.Mutex
	readerCache map[string]cachedReader
	readerCalls map[string]chan struct{}
}

type cachedReader struct {
	capture *Capture
	at      time.Time
	paneID  string
}

func NewWatchTracker() *WatchTracker {
	return &WatchTracker{
		panes:       map[string]paneActivity{},
		readerCache: map[string]cachedReader{},
		readerCalls: map[string]chan struct{}{},
	}
}

func (w *WatchTracker) captureSnapshot(paneID, key, known string, build func() (*Capture, error)) (*Capture, error) {
	for {
		w.readerMu.Lock()
		if cached, ok := w.readerCache[key]; ok && time.Since(cached.at) < readerSnapshotTTL && cached.capture != nil {
			copyOf := *cached.capture
			w.readerMu.Unlock()
			if known != "" && known == copyOf.Digest {
				copyOf.Lines = nil
				copyOf.Unchanged = true
			}
			return &copyOf, nil
		}
		if call, ok := w.readerCalls[key]; ok {
			w.readerMu.Unlock()
			<-call
			continue
		}
		call := make(chan struct{})
		w.readerCalls[key] = call
		w.readerMu.Unlock()

		capture, err := build()

		w.readerMu.Lock()
		delete(w.readerCalls, key)
		if err == nil && !capture.Unchanged {
			copyOf := *capture
			w.readerCache[key] = cachedReader{capture: &copyOf, at: time.Now(), paneID: paneID}
			w.pruneReaderCacheLocked()
		}
		close(call)
		w.readerMu.Unlock()
		return capture, err
	}
}

const maxReaderCacheEntries = 256

func (w *WatchTracker) pruneReaderCacheLocked() {
	cutoff := time.Now().Add(-30 * time.Second)
	for key, cached := range w.readerCache {
		if cached.at.Before(cutoff) {
			delete(w.readerCache, key)
		}
	}
	for len(w.readerCache) > maxReaderCacheEntries {
		var oldestKey string
		var oldest time.Time
		for key, cached := range w.readerCache {
			if oldestKey == "" || cached.at.Before(oldest) {
				oldestKey, oldest = key, cached.at
			}
		}
		delete(w.readerCache, oldestKey)
	}
}

// needsCapture reports whether a pane must be read again.
//
// Reading every pane on every poll is the board's whole cost: one tmux process
// each, several times a second once a few panes exist. tmux already tracks when
// a window last produced output, so a pane in a window that has been silent
// since the last look cannot have changed.
func (w *WatchTracker) needsCapture(paneID string, activity, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.needsCaptureLocked(paneID, activity, now)
}

func (w *WatchTracker) needsCaptureLocked(paneID string, activity, now time.Time) bool {
	p, seen := w.panes[paneID]
	switch {
	case !seen, p.activity.IsZero(), activity.IsZero():
		return true
	case !activity.Equal(p.activity):
		return true
	// Re-read occasionally regardless, so a missed stamp cannot pin the board
	// to a stale tail forever.
	case now.Sub(p.captured) > recaptureAfter:
		return true
	}
	return false
}

type watchCandidate struct {
	id       string
	activity time.Time
}

// captureBatch picks a fair, fixed-size slice of panes whose previews are
// stale. The cursor advances through the whole server so continuously active
// panes at the front cannot starve panes later in the list.
func (w *WatchTracker) captureBatch(candidates []watchCandidate, now time.Time) []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	total := len(candidates)
	if total == 0 {
		w.captureCursor = 0
		return nil
	}

	start := w.captureCursor % total
	selected := make([]string, 0, maxCapturesPerWatch)
	examined := 0
	for examined < total && len(selected) < maxCapturesPerWatch {
		candidate := candidates[(start+examined)%total]
		if w.needsCaptureLocked(candidate.id, candidate.activity, now) {
			selected = append(selected, candidate.id)
		}
		examined++
	}
	w.captureCursor = (start + examined) % total
	return selected
}

// snapshot collapses concurrent viewer polls and briefly reuses their result.
// The board is immutable after construction, so sharing its pointer is safe.
func (w *WatchTracker) snapshot(build func() (*Watch, error)) (*Watch, error) {
	w.snapshotMu.Lock()
	defer w.snapshotMu.Unlock()

	if w.snapshotBoard != nil && time.Since(w.snapshotAt) < watchSnapshotTTL {
		return w.snapshotBoard, nil
	}

	board, err := build()
	if err != nil {
		return nil, err
	}
	w.snapshotBoard = board
	w.snapshotAt = time.Now()
	return board, nil
}

func (w *WatchTracker) layoutSnapshot(build func() (string, error)) (string, error) {
	w.layoutMu.Lock()
	defer w.layoutMu.Unlock()
	if w.layoutRaw != "" && time.Since(w.layoutAt) < 500*time.Millisecond {
		return w.layoutRaw, nil
	}
	raw, err := build()
	if err != nil {
		return "", err
	}
	w.layoutRaw, w.layoutAt = raw, time.Now()
	return raw, nil
}

func (w *WatchTracker) invalidateLayout() {
	w.layoutMu.Lock()
	w.layoutRaw = ""
	w.layoutAt = time.Time{}
	w.layoutMu.Unlock()
}

// cached returns what is already known about a pane whose capture was skipped.
func (w *WatchTracker) cached(paneID string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.panes[paneID].tail
}

// remember stores what a capture produced, for polls that skip the read.
func (w *WatchTracker) remember(paneID string, tail []string, activity, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()

	p := w.panes[paneID]
	p.tail, p.activity, p.captured = tail, activity, now
	w.panes[paneID] = p
}

// readUnchanged reports whether a reader already holds this pane's current
// output, so the expensive capture can be skipped entirely.
//
// Returning the digest to the client saved bandwidth but still paid for the
// capture; a pane in a window tmux says has been silent cannot have changed, so
// the read itself can go too.
func (w *WatchTracker) readUnchanged(paneID, known string, activity time.Time) bool {
	if known == "" || activity.IsZero() {
		return false
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	p, ok := w.panes[paneID]
	return ok && p.readDigest == known && p.readActivity.Equal(activity)
}

// rememberRead stores the fingerprint of a reader capture.
func (w *WatchTracker) rememberRead(paneID, digest string, activity time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()

	p := w.panes[paneID]
	p.readDigest, p.readActivity = digest, activity
	w.panes[paneID] = p
}

// forget drops panes that no longer exist so the map cannot grow without bound.
func (w *WatchTracker) forget(live map[string]bool) {
	w.mu.Lock()
	dead := make(map[string]bool)
	for id := range w.panes {
		if !live[id] {
			delete(w.panes, id)
			dead[id] = true
		}
	}
	w.mu.Unlock()

	if len(dead) > 0 {
		w.readerMu.Lock()
		for key, cached := range w.readerCache {
			if dead[cached.paneID] {
				delete(w.readerCache, key)
			}
		}
		w.readerMu.Unlock()
	}
}

// watchFormat pulls every field the board needs in a single tmux call.
// Tabs separate fields because window and session names may contain almost
// anything else.
const watchFormat = "#{pane_id}\t#{session_name}\t#{window_id}\t#{window_name}\t#{window_index}\t" +
	"#{pane_index}\t#{pane_current_command}\t#{pane_title}\t#{pane_active}\t" +
	"#{window_active}\t#{window_bell_flag}\t#{window_activity}\t#{pid}"

// Watch returns every pane on the server. Concurrent viewers share a snapshot,
// and preview tails are populated with a fixed per-build command budget.
func (c *Controller) Watch() (*Watch, error) {
	return c.watch.snapshot(c.buildWatch)
}

type tmuxRunner func(args ...string) (string, error)

type watchRow struct {
	pane     PaneWatch
	activity time.Time
}

func (c *Controller) buildWatch() (*Watch, error) {
	return c.buildWatchWith(c.runTmux)
}

func (c *Controller) buildWatchWith(run tmuxRunner) (*Watch, error) {
	out, err := run("list-panes", "-a", "-F", watchFormat)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	rows := make([]watchRow, 0)
	live := map[string]bool{}
	checkedServer := false

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		f := splitTmuxFields(line)
		if len(f) < 13 {
			continue
		}

		// Every row carries the same server pid; checking it once per poll
		// costs nothing and catches a tmux restart under us.
		if !checkedServer {
			if pid, err := strconv.Atoi(strings.TrimSpace(f[12])); err == nil {
				c.watch.checkServer(pid)
			}
			checkedServer = true
		}

		paneID := f[0]
		live[paneID] = true

		var activity time.Time
		if ts, err := strconv.ParseInt(strings.TrimSpace(f[11]), 10, 64); err == nil && ts > 0 {
			activity = time.Unix(ts, 0)
		}

		windowIndex, _ := strconv.Atoi(f[4])
		paneIndex, _ := strconv.Atoi(f[5])
		rows = append(rows, watchRow{
			activity: activity,
			pane: PaneWatch{
				ID:           paneID,
				Address:      f[1] + ":" + f[4] + "." + f[5],
				Session:      f[1],
				WindowID:     f[2],
				WindowName:   f[3],
				WindowIndex:  windowIndex,
				PaneIndex:    paneIndex,
				Command:      f[6],
				Title:        f[7],
				Active:       f[8] == "1",
				WindowActive: f[9] == "1",
				Bell:         f[10] == "1",
			},
		})
	}

	candidates := make([]watchCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, watchCandidate{id: row.pane.ID, activity: row.activity})
	}
	captures := map[string]bool{}
	for _, id := range c.watch.captureBatch(candidates, now) {
		captures[id] = true
	}

	board := &Watch{Panes: make([]PaneWatch, 0, len(rows))}
	for _, row := range rows {
		if captures[row.pane.ID] {
			if content, captureErr := run("capture-pane", "-p", "-t", row.pane.ID); captureErr == nil {
				row.pane.Tail = tail(content, tailLines)
				c.watch.remember(row.pane.ID, row.pane.Tail, row.activity, now)
			} else {
				row.pane.Tail = c.watch.cached(row.pane.ID)
			}
		} else {
			row.pane.Tail = c.watch.cached(row.pane.ID)
		}
		board.Panes = append(board.Panes, row.pane)
	}

	c.watch.forget(live)

	// Keep tmux's hierarchy intact.
	sortWatchPanes(board.Panes)
	if data, err := json.Marshal(board.Panes); err == nil {
		h := fnv.New64a()
		_, _ = h.Write(data)
		board.Digest = strconv.FormatUint(h.Sum64(), 36)
	}

	return board, nil
}

func sortWatchPanes(panes []PaneWatch) {
	sort.SliceStable(panes, func(i, j int) bool {
		a, b := panes[i], panes[j]
		if a.Session != b.Session {
			return a.Session < b.Session
		}
		if a.WindowIndex != b.WindowIndex {
			return a.WindowIndex < b.WindowIndex
		}
		return a.PaneIndex < b.PaneIndex
	})
}

// tail returns the last n non-blank-terminated lines of a capture.
func tail(content string, n int) []string {
	lines := strings.Split(strings.TrimRight(content, "\n \t"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	// Blank leading lines waste a card's most valuable rows.
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}

	trimmed := make([]string, 0, len(lines))
	for _, l := range lines {
		trimmed = append(trimmed, strings.TrimRight(l, " \t"))
	}
	return trimmed
}

// checkServer drops pane history when the tmux server has been replaced.
func (w *WatchTracker) checkServer(pid int) {
	if pid == 0 {
		return
	}

	w.readerMu.Lock()
	defer w.readerMu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.serverPID != pid {
		w.panes = map[string]paneActivity{}
		w.readerCache = map[string]cachedReader{}
		w.captureCursor = 0
		w.serverPID = pid
	}
}
