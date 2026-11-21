package bundle

import (
	"bufio"
	"io"
	"net"
	"testing"
	"time"
)

// dial connects to the address of the channel's environment the way the driver does.
func dial(t *testing.T, ch *channel) net.Conn {
	t.Helper()
	var addr string
	for _, kv := range ch.env() {
		if len(kv) > len(channelAddrEnv)+1 && kv[:len(channelAddrEnv)+1] == channelAddrEnv+"=" {
			addr = kv[len(channelAddrEnv)+1:]
		}
	}
	if addr == "" {
		t.Fatalf("env %v has no %s", ch.env(), channelAddrEnv)
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func tokenOf(ch *channel) string { return ch.token }

func newTestChannel(t *testing.T) (*channel, chan struct{}) {
	t.Helper()
	ch, err := newChannel()
	if err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	ch.exited = exited
	t.Cleanup(func() { ch.Close() })
	return ch, exited
}

// The driver proves it is the child by the token in its environment. Whoever connects first
// without it is turned away and does not take the channel.
func TestChannelIgnoresWhoDoesNotKnowTheToken(t *testing.T) {
	ch, _ := newTestChannel(t)
	intruder := dial(t, ch)
	io.WriteString(intruder, "not the token, but as long as it\n"+`{"id":1,"error":"from an intruder"}`+"\n")
	// The intruder is closed by the other side.
	intruder.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := intruder.Read(make([]byte, 1)); err == nil {
		t.Error("the channel kept a connection that sent a wrong token")
	}
	driver := dial(t, ch)
	io.WriteString(driver, tokenOf(ch)+"\n"+`{"id":0,"version":"1"}`+"\n")
	line, err := bufio.NewReader(ch).ReadString('\n')
	if err != nil || line != `{"id":0,"version":"1"}`+"\n" {
		t.Errorf("first answer = %q, %v", line, err)
	}
}

// A connection that never sends the token is dropped after a while, so it cannot hold the
// place of the driver.
func TestChannelDropsASilentConnection(t *testing.T) {
	old := channelProofTimeout
	channelProofTimeout = 100 * time.Millisecond
	t.Cleanup(func() { channelProofTimeout = old })
	ch, _ := newTestChannel(t)
	silent := dial(t, ch)
	silent.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := silent.Read(make([]byte, 1)); err == nil {
		t.Error("the channel kept a silent connection")
	}
	driver := dial(t, ch)
	io.WriteString(driver, tokenOf(ch)+"\nok\n")
	if line, err := bufio.NewReader(ch).ReadString('\n'); err != nil || line != "ok\n" {
		t.Errorf("answer = %q, %v", line, err)
	}
}

// A driver that exits without ever connecting ends the reading, instead of waiting for ever.
func TestChannelEndsWhenTheDriverExitsBeforeConnecting(t *testing.T) {
	ch, exited := newTestChannel(t)
	close(exited)
	if n, err := ch.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		t.Errorf("Read = %d, %v, want 0, EOF", n, err)
	}
}

// The driver dying after it connected ends the reading too.
func TestChannelEndsWhenTheDriverConnectionCloses(t *testing.T) {
	ch, _ := newTestChannel(t)
	driver := dial(t, ch)
	io.WriteString(driver, tokenOf(ch)+"\nlast\n")
	driver.Close()
	got, err := io.ReadAll(ch)
	if err != nil || string(got) != "last\n" {
		t.Errorf("ReadAll = %q, %v", got, err)
	}
}

// Close ends a read that waits, whichever side it waits for.
func TestChannelCloseEndsAWaitingRead(t *testing.T) {
	ch, _ := newTestChannel(t)
	done := make(chan error, 1)
	go func() { _, err := ch.Read(make([]byte, 1)); done <- err }()
	time.Sleep(50 * time.Millisecond)
	ch.Close()
	select {
	case err := <-done:
		if err != io.EOF {
			t.Errorf("Read after Close: %v, want EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not end the read")
	}
}

// setAdmitHook runs f in admit after the proof is checked and before the connection is published.
func setAdmitHook(t *testing.T, f func()) {
	t.Helper()
	admitHook = f
	t.Cleanup(func() { admitHook = nil })
}

// A driver that sends its startup error and exits before admit has published the connection must
// still have its answer read: the user gets the error of Node or Vite, not a bare exit.
func TestChannelReadsTheAnswerOfADriverThatExitedDuringAdmission(t *testing.T) {
	reached, release := make(chan struct{}), make(chan struct{})
	setAdmitHook(t, func() { close(reached); <-release })
	ch, exited := newTestChannel(t)
	driver := dial(t, ch)
	io.WriteString(driver, tokenOf(ch)+"\nstartup error\n")
	driver.Close()
	<-reached
	close(exited)
	go func() { time.Sleep(100 * time.Millisecond); close(release) }()
	got, err := io.ReadAll(ch)
	if err != nil || string(got) != "startup error\n" {
		t.Errorf("ReadAll = %q, %v, want the startup error", got, err)
	}
}

// A connection that proves itself while Close runs is closed, not left open with no reader.
func TestChannelClosesAConnectionAdmittedAfterClose(t *testing.T) {
	reached, release := make(chan struct{}), make(chan struct{})
	setAdmitHook(t, func() { close(reached); <-release })
	ch, _ := newTestChannel(t)
	driver := dial(t, ch)
	io.WriteString(driver, tokenOf(ch)+"\n")
	<-reached
	ch.Close()
	close(release)
	driver.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := driver.Read(make([]byte, 1)); err == nil {
		t.Error("the channel kept a connection admitted after Close")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Error("the connection admitted after Close was left open")
	}
}
