package tmux

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestForgetDropsDeadPanes(t *testing.T) {
	w := NewWatchTracker()
	now := time.Now()
	w.remember("%1", []string{"a"}, now, now)
	w.remember("%2", []string{"b"}, now, now)

	w.forget(map[string]bool{"%1": true})

	if _, ok := w.panes["%2"]; ok {
		t.Error("%2 should have been forgotten")
	}
	if _, ok := w.panes["%1"]; !ok {
		t.Error("%1 should have been kept")
	}
}

func TestWatchCaptureCache(t *testing.T) {
	w := NewWatchTracker()
	now := time.Now()
	activity := now.Add(-time.Second)

	if !w.needsCapture("%1", activity, now) {
		t.Fatal("an unseen pane must be captured")
	}

	want := []string{"latest output"}
	w.remember("%1", want, activity, now)
	if w.needsCapture("%1", activity, now.Add(time.Second)) {
		t.Fatal("an unchanged pane should use its cached tail")
	}
	if got := w.cached("%1"); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("cached tail = %#v, want %#v", got, want)
	}
	if !w.needsCapture("%1", activity, now.Add(recaptureAfter+time.Second)) {
		t.Fatal("a pane must be recaptured periodically")
	}
}

func TestTail(t *testing.T) {
	content := "one\ntwo\nthree\nfour\nfive\n\n\n   \n"
	got := tail(content, 3)
	want := []string{"three", "four", "five"}

	if len(got) != len(want) {
		t.Fatalf("tail = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tail[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSortWatchPanesKeepsTmuxHierarchy(t *testing.T) {
	panes := []PaneWatch{
		{ID: "%4", Session: "work", WindowIndex: 2, PaneIndex: 1},
		{ID: "%1", Session: "other", WindowIndex: 3, PaneIndex: 0},
		{ID: "%3", Session: "work", WindowIndex: 2, PaneIndex: 0},
		{ID: "%2", Session: "work", WindowIndex: 1, PaneIndex: 0},
	}

	sortWatchPanes(panes)

	want := []string{"%1", "%2", "%3", "%4"}
	for i := range want {
		if panes[i].ID != want[i] {
			t.Fatalf("panes[%d] = %s, want %s; got %#v", i, panes[i].ID, want[i], panes)
		}
	}
}

func TestWatchCaptureBatchIsBoundedAndFair(t *testing.T) {
	w := NewWatchTracker()
	now := time.Now()
	candidates := make([]watchCandidate, 1000)
	for i := range candidates {
		candidates[i] = watchCandidate{id: "%" + strconv.Itoa(i+1), activity: now}
	}

	seen := map[string]bool{}
	for round := 0; round < 125; round++ {
		batch := w.captureBatch(candidates, now)
		if len(batch) > maxCapturesPerWatch {
			t.Fatalf("round %d captured %d panes, limit is %d", round, len(batch), maxCapturesPerWatch)
		}
		for _, id := range batch {
			if seen[id] {
				t.Fatalf("pane %s was selected again before every pane received a preview", id)
			}
			seen[id] = true
			w.remember(id, []string{"preview"}, now, now)
		}
	}

	if len(seen) != len(candidates) {
		t.Fatalf("captured %d of %d panes after a full rotation", len(seen), len(candidates))
	}
}

func TestConcurrentWatchSnapshotsShareOneBuild(t *testing.T) {
	w := NewWatchTracker()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32

	build := func() (*Watch, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return &Watch{Panes: []PaneWatch{{ID: "%1"}}}, nil
	}

	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan *Watch, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			board, err := w.snapshot(build)
			if err != nil {
				t.Errorf("snapshot: %v", err)
				return
			}
			results <- board
		}()
	}

	<-started
	close(release)
	wg.Wait()
	close(results)

	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent snapshots ran %d builds, want 1", got)
	}
	for board := range results {
		if len(board.Panes) != 1 || board.Panes[0].ID != "%1" {
			t.Fatalf("snapshot = %#v", board)
		}
	}
}

func TestBuildWatchHasConstantTmuxCommandBudget(t *testing.T) {
	const paneCount = 1000
	rows := make([]string, 0, paneCount)
	for i := 0; i < paneCount; i++ {
		rows = append(rows, strings.Join([]string{
			"%" + strconv.Itoa(i+1),
			"work",
			"@1",
			"large",
			"1",
			strconv.Itoa(i),
			"bash",
			"pane",
			"0",
			"1",
			"0",
			"100",
			"1234",
		}, "\t"))
	}

	calls := 0
	captures := 0
	run := func(args ...string) (string, error) {
		calls++
		switch args[0] {
		case "list-panes":
			return strings.Join(rows, "\n"), nil
		case "capture-pane":
			captures++
			return "preview\n", nil
		default:
			return "", fmt.Errorf("unexpected tmux command %q", args[0])
		}
	}

	controller := &Controller{watch: NewWatchTracker()}
	board, err := controller.buildWatchWith(run)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Panes) != paneCount {
		t.Fatalf("Watch returned %d panes, want %d", len(board.Panes), paneCount)
	}
	if captures != maxCapturesPerWatch {
		t.Fatalf("Watch captured %d pane previews, want %d", captures, maxCapturesPerWatch)
	}
	if calls != 1+maxCapturesPerWatch {
		t.Fatalf("Watch issued %d tmux commands for %d panes, want %d", calls, paneCount, 1+maxCapturesPerWatch)
	}
}

func TestLayoutSnapshotCollapsesConcurrentViewers(t *testing.T) {
	w := NewWatchTracker()
	var builds atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := w.layoutSnapshot(func() (string, error) {
				builds.Add(1)
				time.Sleep(time.Millisecond)
				return "layout", nil
			})
			if err != nil || got != "layout" {
				t.Errorf("layoutSnapshot() = %q, %v", got, err)
			}
		}()
	}
	wg.Wait()
	if got := builds.Load(); got != 1 {
		t.Fatalf("50 concurrent viewers caused %d layout commands, want 1", got)
	}
}

func TestReaderSnapshotReusesPayloadAcrossViewers(t *testing.T) {
	w := NewWatchTracker()
	builds := 0
	first, err := w.captureSnapshot("%1", "%1:500", "", func() (*Capture, error) {
		builds++
		return &Capture{PaneID: "%1", Digest: "same"}, nil
	})
	if err != nil || first.Digest != "same" {
		t.Fatalf("first capture = %#v, %v", first, err)
	}
	second, err := w.captureSnapshot("%1", "%1:500", "same", func() (*Capture, error) {
		builds++
		return nil, fmt.Errorf("must not rebuild")
	})
	if err != nil || !second.Unchanged {
		t.Fatalf("cached capture = %#v, %v", second, err)
	}
	if builds != 1 {
		t.Fatalf("capture built %d times, want 1", builds)
	}
}

func TestReaderSnapshotsForDifferentPanesDoNotShareOneLock(t *testing.T) {
	w := NewWatchTracker()
	started := make(chan string, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for _, paneID := range []string{"%1", "%2"} {
		paneID := paneID
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = w.captureSnapshot(paneID, paneID+":500", "", func() (*Capture, error) {
				started <- paneID
				<-release
				return &Capture{PaneID: paneID, Digest: paneID}, nil
			})
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("different pane captures were serialized")
		}
	}
	close(release)
	wg.Wait()
}

func TestForgetRemovesReaderCacheForDeadPane(t *testing.T) {
	w := NewWatchTracker()
	w.panes["%1"] = paneActivity{}
	w.readerCache["%1:500"] = cachedReader{
		paneID: "%1", capture: &Capture{PaneID: "%1"}, at: time.Now(),
	}
	w.forget(map[string]bool{})
	if len(w.readerCache) != 0 {
		t.Fatalf("reader cache retained dead pane: %#v", w.readerCache)
	}
}
