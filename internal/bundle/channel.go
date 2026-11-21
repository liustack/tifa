package bundle

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"net"
	"sync"
	"time"
)

// Names of the environment variables that tell the driver where to send its answers.
const (
	channelAddrEnv  = "TIFA_DRIVER_ADDR"
	channelTokenEnv = "TIFA_DRIVER_TOKEN"
)

// channelProofTimeout is how long a connection has to send the token before the channel drops it.
var channelProofTimeout = 5 * time.Second

// channel carries the answers of the driver to tifa, one JSON line each. It replaces a file
// descriptor the child inherits, which Windows cannot pass to Node, with a connection on the
// loopback interface that works the same on every system: tifa listens on 127.0.0.1 at a port the
// system picks and tells the driver the address and a random token in the environment. The
// driver connects and sends the token first. Another program of the machine may connect too, so
// the channel takes the first connection that sends the right token and drops the others, and
// stops listening once it has one.
//
// Either side dying ends the reading: the driver's end of the connection closes with its process,
// and tifa closing the channel closes the other end. The requests still go to the driver's stdin,
// whose end is what stops the driver.
type channel struct {
	ln      net.Listener
	token   string
	timeout time.Duration // how long a connection has to send the token
	ready   chan net.Conn // gets the connection that proved itself, once
	done    chan struct{} // closed by Close

	// exited is closed when the driver process has exited. Set it before the first Read.
	exited <-chan struct{}

	mu   sync.Mutex
	conn net.Conn // set by Read once ready has one
	once sync.Once
}

func newChannel() (*channel, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		ln.Close()
		return nil, err
	}
	c := &channel{ln: ln, token: hex.EncodeToString(secret), timeout: channelProofTimeout, ready: make(chan net.Conn, 1), done: make(chan struct{})}
	go c.accept()
	return c, nil
}

// env is what the driver needs to find the channel.
func (c *channel) env() []string {
	return []string{channelAddrEnv + "=" + c.ln.Addr().String(), channelTokenEnv + "=" + c.token}
}

func (c *channel) accept() {
	for {
		conn, err := c.ln.Accept()
		if err != nil {
			return
		}
		go c.admit(conn)
	}
}

// admit keeps conn when its first line is the token, and closes it otherwise.
func (c *channel) admit(conn net.Conn) {
	proof := make([]byte, len(c.token)+1)
	conn.SetReadDeadline(time.Now().Add(c.timeout))
	// Exactly the length of the proof: what the driver sends after it stays for the reader.
	_, err := io.ReadFull(conn, proof)
	if err != nil || proof[len(c.token)] != '\n' || subtle.ConstantTimeCompare(proof[:len(c.token)], []byte(c.token)) != 1 {
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{})
	won := false
	c.once.Do(func() {
		won = true
		c.ready <- conn
		c.ln.Close()
	})
	if !won {
		conn.Close()
	}
}

// Read waits for the driver to connect, then reads what it writes. It ends with io.EOF when the
// driver exited without connecting or the channel is closed.
func (c *channel) Read(p []byte) (int, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		select {
		case conn = <-c.ready:
		case <-c.exited:
			select { // it may have connected and then exited
			case conn = <-c.ready:
			default:
				return 0, io.EOF
			}
		case <-c.done:
			return 0, io.EOF
		}
		c.mu.Lock()
		c.conn = conn
		c.mu.Unlock()
		select { // Close ran meanwhile
		case <-c.done:
			conn.Close()
		default:
		}
	}
	return conn.Read(p)
}

// Close stops listening and closes the connection, which ends a Read that waits.
func (c *channel) Close() error {
	c.mu.Lock()
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	conn := c.conn
	c.mu.Unlock()
	c.ln.Close()
	if conn != nil {
		conn.Close()
	}
	select { // a connection that proved itself and was never read
	case conn := <-c.ready:
		conn.Close()
	default:
	}
	return nil
}
