package transport

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"time"

	"github.com/atotto/clipboard"
	"github.com/mattn/go-shellwords"
	"github.com/svent/go-nbreader"
)

type Transport interface {
	Read() (string, error)
	Write(text string) error
	Reset()
}

type Clipboard struct{}

func (c *Clipboard) Read() (string, error) {
	return clipboard.ReadAll()

}

func (c *Clipboard) Write(text string) error {
	return clipboard.WriteAll(text)
}

func (c *Clipboard) Reset() {
	clipboard.WriteAll(strconv.FormatInt(time.Now().UnixNano(), 10))
}

type Command struct {
	cmd        *exec.Cmd
	stdin      io.Writer
	stdout     io.Reader
	bufferSize int
}

func NewCommand(cmd string, bufferSize int, interval time.Duration) (*Command, error) {
	var err error
	args, err := shellwords.Parse(cmd)
	if err != nil {
		return nil, fmt.Errorf("cannot parse transport command: %s", err)
	}
	var c *Command
	if len(args) > 1 {
		c = &Command{cmd: exec.Command(args[0], args[1:]...)}
	} else {
		c = &Command{cmd: exec.Command(cmd)}
	}
	c.bufferSize = bufferSize
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("cannot open stdin for transport command: %s", err)
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cannot open stdout for transport command: %s", err)
	}
	c.stdout = nbreader.NewNBReader(stdout, bufferSize, nbreader.ChunkTimeout(interval/2), nbreader.Timeout(interval*4/5))
	err = c.cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("cannot execute transport command: %s", err)
	}
	return c, nil
}

func (c *Command) Read() (string, error) {
	b := make([]byte, c.bufferSize*2)
	n, err := c.stdout.Read(b)
	return string(b[:n]), err
}

func (c *Command) Write(text string) error {
	_, err := c.stdin.Write([]byte(text))
	return err
}

func (c *Command) Reset() {
}

type TCPConn struct {
	conn       net.Conn
	reader     io.Reader
	bufferSize int
}

func DialTCP(addr string, bufferSize int, interval time.Duration) (*TCPConn, error) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("cannot dial connection: %s", err)
	}
	stdout := nbreader.NewNBReader(c, bufferSize, nbreader.ChunkTimeout(interval/2), nbreader.Timeout(interval*4/5))
	return &TCPConn{c, stdout, bufferSize}, nil
}

func ListenTCP(addr string, bufferSize int, interval time.Duration) (*TCPConn, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("cannot start listener: %s", err)
	}
	c, err := l.Accept()
	if err != nil {
		return nil, fmt.Errorf("cannot accept tcp connection: %s", err)
	}
	r := nbreader.NewNBReader(c, bufferSize, nbreader.ChunkTimeout(interval/2), nbreader.Timeout(interval*4/5))
	return &TCPConn{c, r, bufferSize}, nil
}

func (c *TCPConn) Read() (string, error) {
	b := make([]byte, c.bufferSize*2)
	n, err := c.reader.Read(b)
	return string(b[:n]), err
}

func (c *TCPConn) Write(text string) error {
	_, err := c.conn.Write([]byte(text))
	return err
}

func (c *TCPConn) Reset() {
}
