// Package keys describes the on-screen key panel offered to touch clients.
//
// A phone has no Ctrl or Alt key, so every tmux binding that relies on a
// modifier is unreachable by typing. Instead of emulating a modifier keyboard,
// the panel sends the raw escape sequence for a binding directly, which lets a
// single tap stand in for a chord.
//
// The default panel mirrors the convention of encoding tmux actions as
// modified F5-F12 keys. Terminals report those as CSI sequences of the form
//
//	ESC [ <code> ; <modifier> ~
//
// where code is 15/17/18/19/20/21/23/24 for F5-F12 and modifier is
// 1 + 1(shift) + 2(alt) + 4(ctrl), giving 5 for Ctrl, 6 for Ctrl+Shift and
// 7 for Ctrl+Alt. tmux decodes these without needing extended-keys.
package keys

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

// Key is a single button on the panel.
type Key struct {
	// Label is the text shown on the button.
	Label string `json:"label"`
	// Seq is the raw byte sequence written to the TTY when tapped.
	Seq string `json:"seq"`
	// Key names the chord this button stands for, for display only.
	Key string `json:"key,omitempty"`
	// Hint describes the resulting action, shown as a tooltip/subtitle.
	Hint string `json:"hint,omitempty"`
	// Wide makes the button span two columns.
	Wide bool `json:"wide,omitempty"`
}

// Group is a labelled row of related keys.
type Group struct {
	Label string `json:"label"`
	Keys  []Key  `json:"keys"`
}

// Panel is the whole configurable key drawer.
type Panel struct {
	// Row appears in the first "Keys" tab. The name is retained for config
	// compatibility with older versions that rendered it persistently.
	Row []Key `json:"row"`
	// Groups are the remaining labelled tabs.
	Groups []Group `json:"groups"`
}

// fkey builds the CSI sequence for a modified F5-F12 key.
// code is the xterm function-key code, mod is the CSI modifier parameter.
func fkey(code, mod int) string {
	return "\x1b[" + itoa(code) + ";" + itoa(mod) + "~"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// xterm function-key codes for F5-F12.
const (
	f5, f6, f7, f8 = 15, 17, 18, 19
	f9, f10, f11   = 20, 21, 23
	f12            = 24
	ctrl           = 5 // Ctrl
	ctrlShift      = 6 // Ctrl+Shift
	ctrlAlt        = 7 // Ctrl+Alt
)

// Default returns the built-in panel.
func Default() *Panel {
	return &Panel{Row: DefaultRow(), Groups: []Group{
		{Label: "Pane", Keys: []Key{
			{Label: "←", Seq: fkey(f5, ctrl), Key: "C-F5", Hint: "选择左侧 Pane"},
			{Label: "↓", Seq: fkey(f6, ctrl), Key: "C-F6", Hint: "选择下方 Pane"},
			{Label: "↑", Seq: fkey(f7, ctrl), Key: "C-F7", Hint: "选择上方 Pane"},
			{Label: "→", Seq: fkey(f8, ctrl), Key: "C-F8", Hint: "选择右侧 Pane"},
			{Label: "⛶ Zoom", Seq: fkey(f9, ctrl), Key: "C-F9", Hint: "切换 Pane 全屏"},
			{Label: "─ 分屏", Seq: fkey(f10, ctrl), Key: "C-F10", Hint: "上下分屏"},
			{Label: "│ 分屏", Seq: fkey(f11, ctrl), Key: "C-F11", Hint: "左右分屏"},
		}},
		{Label: "Window", Keys: []Key{
			{Label: "选择", Seq: fkey(f5, ctrlShift), Key: "C-S-F5", Hint: "choose-tree -Zw"},
			{Label: "新建", Seq: fkey(f6, ctrlShift), Key: "C-S-F6", Hint: "新建 Window"},
			{Label: "重命名", Seq: fkey(f7, ctrlShift), Key: "C-S-F7", Hint: "重命名 Window"},
			{Label: "关闭", Seq: fkey(f8, ctrlShift), Key: "C-S-F8", Hint: "关闭 Window"},
			{Label: "‹ 上一个", Seq: fkey(f10, ctrlShift), Key: "C-S-F10", Hint: "previous-window"},
			{Label: "下一个 ›", Seq: fkey(f9, ctrlShift), Key: "C-S-F9", Hint: "next-window"},
		}},
		{Label: "Session", Keys: []Key{
			{Label: "选择", Seq: fkey(f12, ctrl), Key: "C-F12", Hint: "choose-tree -Zs"},
			{Label: "新建", Seq: fkey(f5, ctrlAlt), Key: "C-M-F5", Hint: "新建 Session"},
			{Label: "重命名", Seq: fkey(f6, ctrlAlt), Key: "C-M-F6", Hint: "重命名 Session"},
			{Label: "关闭", Seq: fkey(f7, ctrlAlt), Key: "C-M-F7", Hint: "关闭 Session"},
			{Label: "‹ 上一个", Seq: fkey(f9, ctrlAlt), Key: "C-M-F9", Hint: "上一个 Session"},
			{Label: "下一个 ›", Seq: fkey(f8, ctrlAlt), Key: "C-M-F8", Hint: "下一个 Session"},
		}},
		{Label: "其他", Keys: []Key{
			{Label: "命令中心", Seq: fkey(f11, ctrlAlt), Key: "C-M-F11", Hint: "display-menu", Wide: true},
			{Label: "Copy Mode", Seq: fkey(f12, ctrlAlt), Key: "C-M-F12", Hint: "进入复制模式", Wide: true},
			{Label: "Popup Shell", Seq: fkey(f12, ctrlShift), Key: "C-S-F12", Hint: "弹出 Shell", Wide: true},
			{Label: "SSH", Seq: fkey(f11, ctrlShift), Key: "C-S-F11", Hint: "SSH 交接", Wide: true},
			{Label: "↵ 换行", Seq: fkey(f10, ctrlAlt), Key: "C-M-F10", Hint: "发送 C-j，插入换行而不提交", Wide: true},
		}},
		// Readline's editing chords. A phone can reach these through the
		// row's sticky Ctrl, but naming them saves guessing which letter.
		{Label: "编辑", Keys: []Key{
			{Label: "行首", Seq: "\x01", Key: "C-a"},
			{Label: "行尾", Seq: "\x05", Key: "C-e"},
			{Label: "删到行尾", Seq: "\x0b", Key: "C-k"},
			{Label: "删到行首", Seq: "\x15", Key: "C-u"},
			{Label: "删前一词", Seq: "\x17", Key: "C-w"},
			{Label: "历史搜索", Seq: "\x12", Key: "C-r"},
			{Label: "上一条", Seq: "\x10", Key: "C-p"},
			{Label: "下一条", Seq: "\x0e", Key: "C-n"},
			{Label: "tmux 前缀", Seq: "\x02", Key: "C-b"},
			{Label: "Insert", Seq: "\x1b[2~"},
			{Label: "F1", Seq: "\x1bOP"},
			{Label: "F2", Seq: "\x1bOQ"},
			{Label: "F3", Seq: "\x1bOR"},
			{Label: "F4", Seq: "\x1bOS"},
		}},
	}}
}

// DefaultRow returns the built-in first tab: keys a soft keyboard lacks.
func DefaultRow() []Key {
	return []Key{
		{Label: "ESC", Seq: "\x1b"},
		{Label: "TAB", Seq: "\t"},
		{Label: "←", Seq: "\x1b[D"},
		{Label: "↓", Seq: "\x1b[B"},
		{Label: "↑", Seq: "\x1b[A"},
		{Label: "→", Seq: "\x1b[C"},
		{Label: "⌫", Seq: "\x7f", Hint: "Backspace"},
		{Label: "DEL", Seq: "\x1b[3~", Hint: "向后删除"},
		{Label: "HOME", Seq: "\x1b[H"},
		{Label: "END", Seq: "\x1b[F"},
		{Label: "PGUP", Seq: "\x1b[5~"},
		{Label: "PGDN", Seq: "\x1b[6~"},
		{Label: "^C", Seq: "\x03", Hint: "中断"},
		{Label: "^D", Seq: "\x04", Hint: "EOF"},
		{Label: "^Z", Seq: "\x1a", Hint: "挂起"},
		{Label: "^L", Seq: "\x0c", Hint: "清屏"},
		{Label: "↵⇧", Seq: "\x1b[13;2u", Hint: "Shift+Enter：换行不提交"},
	}
}

// Load reads a panel from a JSON file, falling back to Default when path is
// empty. Escape sequences are written as JSON string escapes, e.g. "[20;5~".
func Load(path string) (*Panel, error) {
	if path == "" {
		return Default(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read key panel file %s", path)
	}

	var panel Panel
	if err := json.Unmarshal(data, &panel); err != nil {
		return nil, errors.Wrapf(err, "failed to parse key panel file %s", path)
	}
	if err := panel.Validate(); err != nil {
		return nil, errors.Wrapf(err, "invalid key panel file %s", path)
	}

	return &panel, nil
}

// Validate checks the fields required to render and send every configured key.
func (p *Panel) Validate() error {
	validate := func(location string, key Key) error {
		if key.Label == "" {
			return fmt.Errorf("%s label is empty", location)
		}
		if key.Seq == "" {
			return fmt.Errorf("%s sequence is empty", location)
		}
		return nil
	}

	for i, key := range p.Row {
		if err := validate(fmt.Sprintf("row[%d]", i), key); err != nil {
			return err
		}
	}
	for i, group := range p.Groups {
		if group.Label == "" {
			return fmt.Errorf("groups[%d] label is empty", i)
		}
		for j, key := range group.Keys {
			if err := validate(fmt.Sprintf("groups[%d].keys[%d]", i, j), key); err != nil {
				return err
			}
		}
	}
	return nil
}

// Save validates and atomically replaces the configured panel file.
func Save(path string, panel *Panel) error {
	if path == "" {
		return errors.New("no key panel file configured")
	}
	if err := panel.Validate(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(panel, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to encode key panel")
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".webtmux-keys-*.tmp")
	if err != nil {
		return errors.Wrapf(err, "failed to create temporary key panel beside %s", path)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return errors.Wrap(err, "failed to set key panel permissions")
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return errors.Wrap(err, "failed to write key panel")
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return errors.Wrap(err, "failed to sync key panel")
	}
	if err := tmp.Close(); err != nil {
		return errors.Wrap(err, "failed to close key panel")
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return errors.Wrapf(err, "failed to replace key panel %s", path)
	}
	return nil
}
