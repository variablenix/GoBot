package bot

import (
	"bytes"
	"errors"
	"net"
	"sync"
	"time"
)

// Leave room for IRCv3 server message tags (8191 bytes including framing)
// plus the ordinary 512-byte IRC line. Allow extra room for servers with
// larger messages, while bounding the IRC library's ReadString allocation.
const maxIRCWireLine = 16 << 10

var errIRCLineTooLong = errors.New("IRC line exceeds receive limit")

type boundedIRCConn struct {
	net.Conn
	lineBytes int // only the IRC reader touches this
	oversized bool
	writeMu   sync.Mutex
}

func (c *boundedIRCConn) Read(p []byte) (int, error) {
	if c.oversized {
		return 0, errIRCLineTooLong
	}
	n, err := c.Conn.Read(p)
	start := 0
	for start < n {
		end := n
		newline := bytes.IndexByte(p[start:n], '\n')
		if newline >= 0 {
			end = start + newline + 1
		}
		if end-start > maxIRCWireLine-c.lineBytes {
			// The parser never receives bytes from the oversized portion.
			c.oversized = true
			return start, errIRCLineTooLong
		}
		c.lineBytes += end - start
		if newline >= 0 {
			c.lineBytes = 0
		}
		start = end
	}
	return n, err
}

func (c *boundedIRCConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}
