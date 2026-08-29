package webtty

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"webtmux/pkg/tmux"
)

func TestInitialization(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	mMaster, _, _, cancel := prepareSUT(t, &wg)
	defer cancel()

	// Check that the initialization happens as expected
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetWindowTitle)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetBufferSize)
}

func TestInitializationWithPreferences(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	mMaster, _, _, cancel := prepareSUT(t, &wg, WithMasterPreferences(map[string]string{"foo": "bar"}))
	defer cancel()

	// Check that the initialization happens as expected
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetWindowTitle)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetBufferSize)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetPreferences)
}

func TestInitializationWithReconnect(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	mMaster, _, _, cancel := prepareSUT(t, &wg, WithReconnect(10))
	defer cancel()

	// Check that the initialization happens as expected
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetWindowTitle)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetBufferSize)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetReconnect)
}

func TestWriteFromSlaveCommand(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	mMaster, mSlave, _, cancel := prepareSUT(t, &wg)
	defer cancel()

	// Check that the initialization happens as expected
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetWindowTitle)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetBufferSize)

	// Simulate the slave (the process being run by GoTTY)
	// echoing "foobar"
	message := []byte("foobar")
	mSlave.slaveToGottyWriter.Write(message)

	// And then make sure it makes it way to the client
	// through the websocket as an output message
	buf := make([]byte, 1024)
	n, err := mMaster.gottyToMasterReader.Read(buf)
	if err != nil {
		t.Fatalf("Unexpected error from Read(): %s", err)
	}
	if buf[0] != Output {
		t.Fatalf("Unexpected message type `%c`", buf[0])
	}

	if !bytes.Equal(buf[1:n], message) {
		t.Fatalf("Unexpected message received: `%s`", buf[1:n])
	}

	cancel()
	wg.Wait()
}
func TestWriteFromFrontend(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	mMaster, mSlave, _, cancel := prepareSUT(t, &wg, WithPermitWrite())
	defer cancel()

	// Absorb initialization messages
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetWindowTitle)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetBufferSize)

	// simulate input from frontend...
	message := []byte("1hello\n") // line buffered canonical mode
	mMaster.masterToGottyWriter.Write(message)

	// ...and make sure it makes it through to the slave intact
	readBuf := make([]byte, 1024)
	n, err := mSlave.gottyToSlaveReader.Read(readBuf)
	if err != nil {
		t.Fatalf("Unexpected error from Write(): %s", err)
	}
	if !bytes.Equal(readBuf[:n], message[1:]) {
		t.Fatalf("Unexpected message received: `%s`", readBuf[:n])
	}
}

func TestCtrlCFromFrontend(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	mMaster, mSlave, _, cancel := prepareSUT(t, &wg, WithPermitWrite())
	defer cancel()
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetWindowTitle)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetBufferSize)

	if _, err := mMaster.masterToGottyWriter.Write([]byte("4base64")); err != nil {
		t.Fatal(err)
	}
	// The browser sends protocol type '1' followed by base64(0x03).
	if _, err := mMaster.masterToGottyWriter.Write([]byte("1Aw==")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1)
	if _, err := mSlave.gottyToSlaveReader.Read(buffer); err != nil {
		t.Fatal(err)
	}
	if buffer[0] != 0x03 {
		t.Fatalf("PTY received %#x, want Ctrl+C (0x03)", buffer[0])
	}
}

func TestSlowTmuxRequestDoesNotBlockTerminalInput(t *testing.T) {
	mMaster, mSlave := newMockMaster(), newMockSlave()
	wt, err := New(mMaster, mSlave, WithPermitWrite())
	if err != nil {
		t.Fatal(err)
	}
	ctrl := &blockingTmuxController{watchStarted: make(chan struct{}), release: make(chan struct{})}
	wt.SetTmuxController(ctrl)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wt.Run(ctx)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetWindowTitle)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetBufferSize)

	if _, err := mMaster.masterToGottyWriter.Write([]byte{TmuxWatch}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctrl.watchStarted:
	case <-time.After(time.Second):
		t.Fatal("tmux watch did not start")
	}
	if _, err := mMaster.masterToGottyWriter.Write([]byte{'1', 'x'}); err != nil {
		t.Fatal(err)
	}
	got := make(chan byte, 1)
	go func() {
		buffer := make([]byte, 1)
		_, _ = mSlave.gottyToSlaveReader.Read(buffer)
		got <- buffer[0]
	}()
	select {
	case value := <-got:
		if value != 'x' {
			t.Fatalf("PTY received %q", value)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal input was blocked behind tmux watch")
	}
	close(ctrl.release)
	checkNextMsgType(t, mMaster.gottyToMasterReader, TmuxWatchUpdate)
}

type blockingTmuxController struct {
	watchStarted chan struct{}
	release      chan struct{}
}

func (c *blockingTmuxController) GetLayout() *tmux.Layout     { return nil }
func (c *blockingTmuxController) RefreshLayout() error        { return nil }
func (c *blockingTmuxController) SelectPane(string) error     { return nil }
func (c *blockingTmuxController) SelectWindow(string) error   { return nil }
func (c *blockingTmuxController) SwitchSession(string) error  { return nil }
func (c *blockingTmuxController) SplitPane(bool) error        { return nil }
func (c *blockingTmuxController) ClosePane(string) error      { return nil }
func (c *blockingTmuxController) ZoomPane(string, bool) error { return nil }
func (c *blockingTmuxController) GotoPane(string) error       { return nil }
func (c *blockingTmuxController) Capture(string, int, string) (*tmux.Capture, error) {
	return &tmux.Capture{}, nil
}
func (c *blockingTmuxController) EnterCopyMode() error      { return nil }
func (c *blockingTmuxController) ExitCopyMode() error       { return nil }
func (c *blockingTmuxController) ScrollUp(int) error        { return nil }
func (c *blockingTmuxController) ScrollDown(int) error      { return nil }
func (c *blockingTmuxController) NewWindow() error          { return nil }
func (c *blockingTmuxController) Events() <-chan tmux.Event { return nil }
func (c *blockingTmuxController) Watch() (*tmux.Watch, error) {
	close(c.watchStarted)
	<-c.release
	return &tmux.Watch{}, nil
}

func TestPing(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	mMaster, _, _, cancel := prepareSUT(t, &wg)
	defer cancel()

	// Absorb initialization messages
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetWindowTitle)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetBufferSize)

	// ping
	message := []byte("2\n") // line buffered canonical mode
	n, err := mMaster.masterToGottyWriter.Write(message)
	if err != nil {
		t.Fatalf("Unexpected error from Write(): %s", err)
	}
	if n != len(message) {
		t.Fatalf("Write() accepted `%d` for message `%s`", n, message)
	}

	readBuf := make([]byte, 1024)
	n, err = mMaster.gottyToMasterReader.Read(readBuf)
	if err != nil {
		t.Fatalf("Unexpected error from Read(): %s", err)
	}
	if !bytes.Equal(readBuf[:n], []byte{'2'}) {
		t.Fatalf("Unexpected message received: `%s`", readBuf[:n])
	}

	cancel()
	wg.Wait()
}

func TestResizeTerminal(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	mMaster, mSlave, _, cancel := prepareSUT(t, &wg)
	defer cancel()

	// Absorb initialization messages
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetWindowTitle)
	checkNextMsgType(t, mMaster.gottyToMasterReader, SetBufferSize)

	message := []byte(`3{"Columns": 123, "Rows": 45}` + "\n") // line buffered canonical mode

	mSlave.wg.Add(1)
	n, err := mMaster.masterToGottyWriter.Write(message)
	if err != nil {
		t.Fatalf("Unexpected error from Write(): %s", err)
	}
	if n != len(message) {
		t.Fatalf("Write() accepted `%d` for message `%s`", n, message)
	}
	mSlave.wg.Wait()

	if mSlave.columns != 123 {
		t.Fatalf("Columns not set correctly. Expected %v, got %v", 123, mSlave.columns)
	}

	if mSlave.rows != 45 {
		t.Fatalf("Rows not set correctly. Expected %v, got %v", 45, mSlave.columns)
	}

	cancel()
	wg.Wait()
}

type mockMaster struct {
	gottyToMasterReader *io.PipeReader
	gottyToMasterWriter *io.PipeWriter
	masterToGottyReader *io.PipeReader
	masterToGottyWriter *io.PipeWriter
}

type mockSlave struct {
	gottyToSlaveReader *io.PipeReader
	gottyToSlaveWriter *io.PipeWriter
	slaveToGottyReader *io.PipeReader
	slaveToGottyWriter *io.PipeWriter
	wg                 sync.WaitGroup
	columns, rows      int
}

func prepareSUT(t *testing.T, wg *sync.WaitGroup, options ...Option) (*mockMaster, *mockSlave, *WebTTY, context.CancelFunc) {
	mMaster := newMockMaster()
	mSlave := newMockSlave()

	dt, err := New(mMaster, mSlave, options...)
	if err != nil {
		t.Fatalf("Unexpected error from New(): %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg.Add(1)
	go func() {
		wg.Done()
		dt.Run(ctx)
	}()
	return mMaster, mSlave, dt, cancel
}

func checkNextMsgType(t *testing.T, reader io.Reader, expected byte) {
	msgType, _ := nextMsg(t, reader)
	if msgType != expected {
		t.Fatalf("Unexpected message type `%c`", msgType)
	}
}

func nextMsg(t *testing.T, reader io.Reader) (byte, []byte) {
	buf := make([]byte, 1024)
	_, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	return buf[0], buf[1:]
}

func newMockMaster() *mockMaster {
	rv := &mockMaster{}
	rv.gottyToMasterReader, rv.gottyToMasterWriter = io.Pipe()
	rv.masterToGottyReader, rv.masterToGottyWriter = io.Pipe()
	return rv
}

func (mm *mockMaster) close() {
	mm.masterToGottyWriter.Close()
	mm.gottyToMasterReader.Close()
}

func (mm *mockMaster) Read(buf []byte) (int, error) {
	return mm.masterToGottyReader.Read(buf)
}

func (mm *mockMaster) Write(buf []byte) (int, error) {
	return mm.gottyToMasterWriter.Write(buf)
}

func newMockSlave() *mockSlave {
	rv := &mockSlave{}
	rv.gottyToSlaveReader, rv.gottyToSlaveWriter = io.Pipe()
	rv.slaveToGottyReader, rv.slaveToGottyWriter = io.Pipe()
	return rv
}

func (ms *mockSlave) close() {
	ms.slaveToGottyWriter.Close()
	ms.gottyToSlaveReader.Close()
}

func (ms *mockSlave) Read(buf []byte) (int, error) {
	return ms.slaveToGottyReader.Read(buf)
}

func (ms *mockSlave) Write(buf []byte) (int, error) {
	return ms.gottyToSlaveWriter.Write(buf)
}

func (ms *mockSlave) WindowTitleVariables() map[string]interface{} {
	return nil
}

func (ms *mockSlave) ResizeTerminal(columns int, rows int) error {
	ms.columns = columns
	ms.rows = rows
	ms.wg.Done()
	return nil
}
