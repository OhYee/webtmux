package tmux

import (
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"webtmux/pkg/ansi"
)

// How many lines of scrollback the reader pulls. Enough to scroll back through
// a build or a conversation without shipping a whole session's history to a
// phone on every poll.
const defaultCaptureLines = 500

// maxCaptureLines bounds what a client can ask for by default.
//
// This exists to bound transfer, not because tmux minds: a request is answered
// in milliseconds either way, but five thousand lines is already 400KB of JSON
// and every extension re-sends the whole range. It is deliberately adjustable,
// because the right ceiling depends on whether the phone is on wifi or a train.
var maxCaptureLines = 5000

// SetMaxCaptureLines adjusts how far back a reader may ask.
func SetMaxCaptureLines(n int) {
	if n > 0 {
		maxCaptureLines = n
	}
}

// Capture is a pane's output prepared for reflowing in a browser.
type Capture struct {
	PaneID  string      `json:"paneId"`
	Address string      `json:"address"`
	Command string      `json:"command"`
	Lines   []ansi.Line `json:"lines"`
	// Digest fingerprints the captured output. A reader polls every second or
	// so while its pane may say nothing for minutes, and the payload runs to
	// tens of kilobytes; echoing the digest back lets an unchanged capture cost
	// one small message instead of the whole screen again.
	Digest string `json:"digest"`
	// Unchanged is set instead of Lines when the digest still matches.
	Unchanged bool `json:"unchanged,omitempty"`
	// Requested is the window that was asked for and Available is everything
	// tmux still holds. Together they tell the reader whether scrolling further
	// back would find anything, so it can stop asking at the true beginning
	// rather than at an arbitrary number.
	Requested int `json:"requested"`
	Available int `json:"available"`
	// Capped reports that this server, not tmux, is what stopped the reader
	// going further back. Without it the UI cannot tell "this is the beginning
	// of the output" from "this is as far as I am willing to send", and it was
	// claiming the former while tmux still held tens of thousands of lines.
	Capped bool `json:"capped,omitempty"`
}

// Capture reads a pane's output as logical lines with their colours.
//
// -J is what makes this worth doing: tmux hard-wraps every line at the pane
// width, and -J rejoins those fragments back into the line the program
// actually wrote. Handing the browser logical lines lets it wrap them to the
// phone's width instead of the terminal's.
func (c *Controller) Capture(paneID string, lines int, known string) (*Capture, error) {
	if lines <= 0 {
		lines = defaultCaptureLines
	}
	capped := false
	if lines > maxCaptureLines {
		lines, capped = maxCaptureLines, true
	}
	key := paneID + ":" + strconv.Itoa(lines)
	return c.watch.captureSnapshot(key, known, func() (*Capture, error) {
		return c.captureFresh(paneID, lines, known, capped)
	})
}

func (c *Controller) captureFresh(paneID string, lines int, known string, capped bool) (*Capture, error) {
	meta, err := c.runTmux("display-message", "-t", paneID, "-p",
		"#{session_name}:#{window_index}.#{pane_index}\t#{pane_current_command}\t#{history_size}\t#{pane_height}\t#{window_activity}")
	if err != nil {
		return nil, err
	}

	fields := splitTmuxFields(trimNewline(meta))
	address, command := field(fields, 0), field(fields, 1)
	history, _ := strconv.Atoi(field(fields, 2))
	height, _ := strconv.Atoi(field(fields, 3))

	var activity time.Time
	if ts, convErr := strconv.ParseInt(strings.TrimSpace(field(fields, 4)), 10, 64); convErr == nil && ts > 0 {
		activity = time.Unix(ts, 0)
	}

	unchanged := &Capture{
		Capped:    capped,
		PaneID:    paneID,
		Address:   address,
		Command:   command,
		Digest:    known,
		Unchanged: true,
		Requested: lines,
		Available: history + height,
	}

	// Skip the read entirely when the window has produced nothing since the
	// client's copy was taken. capture-pane over hundreds of lines is the most
	// expensive thing this server asks tmux to do.
	if c.watch.readUnchanged(paneID, known, activity) {
		return unchanged, nil
	}

	out, err := c.runTmux("capture-pane", "-p", "-e", "-J",
		"-S", "-"+strconv.Itoa(lines), "-t", paneID)
	if err != nil {
		return nil, err
	}

	h := fnv.New64a()
	h.Write([]byte(out))
	digest := strconv.FormatUint(h.Sum64(), 36)
	c.watch.rememberRead(paneID, digest, activity)

	if known != "" && known == digest {
		unchanged.Digest = digest
		return unchanged, nil
	}

	return &Capture{
		Capped:    capped,
		Digest:    digest,
		PaneID:    paneID,
		Address:   address,
		Command:   command,
		Lines:     trimBlankEdges(ansi.Parse(out)),
		Requested: lines,
		Available: history + height,
	}, nil
}

func field(fields []string, i int) string {
	if i < len(fields) {
		return fields[i]
	}
	return ""
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// trimBlankEdges drops the empty rows tmux pads a pane with, which would
// otherwise render as a wall of blank space above and below the content.
func trimBlankEdges(lines []ansi.Line) []ansi.Line {
	blank := func(l ansi.Line) bool {
		for _, s := range l {
			for _, r := range s.Text {
				if r != ' ' && r != '\t' {
					return false
				}
			}
		}
		return true
	}

	start := 0
	for start < len(lines) && blank(lines[start]) {
		start++
	}
	end := len(lines)
	for end > start && blank(lines[end-1]) {
		end--
	}
	return lines[start:end]
}
