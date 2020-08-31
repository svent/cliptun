package channel

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	mrand "math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/svent/cliptun/transport"
	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/pbkdf2"
)

const (
	QueueSize = 16
)

type CBPacketType int

const (
	PacketTypeData CBPacketType = iota
	PacketTypeControl
)

type CBPacket struct {
	Target  PeerType
	Type    CBPacketType
	Payload string
	Seq     int
	Ack     int
}

type channelOpenDirectMsg struct {
	Raddr string
	Rport uint32
	Laddr string
	Lport uint32
}

type PeerType int

const (
	CLIENT PeerType = iota
	SERVER
)

var (
	netTimeout  = 50 * time.Millisecond
	errorLogger = log.New(ioutil.Discard, "", 0)
	debugLogger = log.New(ioutil.Discard, "", 0)
	traceLogger = log.New(ioutil.Discard, "", 0)
)

type Channel struct {
	interval   time.Duration
	bufferSize int

	transport transport.Transport

	receiveQueue      map[int]CBPacket
	receiveChan       chan CBPacket
	receiveQueueIndex int

	sendQueue      map[int]CBPacket
	sendChan       chan CBPacket
	sendQueueIndex int

	ownHeader       PeerType
	peerHeader      PeerType
	secretKey       [32]byte
	delayedShutdown sync.Once

	controlPacketCallback ControlPacketCallback
}

type ControlPacketCallback func(cmd, arg string)

type ChannelOptions struct {
	ControlPacketCallback ControlPacketCallback
	Interval              time.Duration
	Password              string
	Transport             string
	Blocksize             int
	ErrorLogger           *log.Logger
	DebugLogger           *log.Logger
	TraceLogger           *log.Logger
}

func NewChannel(typ PeerType, options ChannelOptions) (*Channel, error) {
	if options.ErrorLogger != nil {
		errorLogger = options.ErrorLogger
	}
	if options.DebugLogger != nil {
		debugLogger = options.DebugLogger
	}
	if options.TraceLogger != nil {
		traceLogger = options.TraceLogger
	}

	c := Channel{}
	c.controlPacketCallback = options.ControlPacketCallback

	c.receiveQueue = make(map[int]CBPacket)
	c.receiveChan = make(chan CBPacket)
	c.receiveQueueIndex = -1

	c.sendQueue = make(map[int]CBPacket)
	c.sendChan = make(chan CBPacket, QueueSize)
	c.sendQueueIndex = -1

	if options.Interval > 0 {
		c.interval = options.Interval
	}

	c.bufferSize = options.Blocksize

	debugLogger.Println("using transport:", options.Transport)
	if options.Transport == "" || options.Transport == "clipboard" {
		c.transport = &transport.Clipboard{}
	} else {
		if strings.HasPrefix(options.Transport, "exec=") {
			var err error
			c.transport, err = transport.NewCommand(options.Transport[5:], c.bufferSize*2, c.interval)
			if err != nil {
				return nil, fmt.Errorf("cannot create transport: %s", err)
			}
		} else if strings.HasPrefix(options.Transport, "tcp=") {
			t, err := transport.DialTCP(options.Transport[4:], c.bufferSize*2, c.interval)
			if err != nil {
				return nil, fmt.Errorf("cannot dial tcp connection: %s", err)
			}
			c.transport = t
		} else if strings.HasPrefix(options.Transport, "tcp-listen=") {
			t, err := transport.ListenTCP(options.Transport[11:], c.bufferSize*2, c.interval)
			if err != nil {
				return nil, fmt.Errorf("cannot dial tcp connection: %s", err)
			}
			c.transport = t
		} else {
			return nil, fmt.Errorf("unknown transport method")
		}
	}

	if options.Password == "" {
		errorLogger.Fatalln("no password for encryption given")
	}
	// use a static salt as we need the same hash on both sides of the tunnel
	salt := []byte{'c', 'l', 'i', 'p', 't', 'u', 'n', 0}
	key := pbkdf2.Key([]byte(options.Password), salt, 4096, 32, sha256.New)
	n := copy(c.secretKey[:], key)
	if n != 32 {
		return nil, fmt.Errorf("could not derive key from password")
	}

	if typ == CLIENT {
		c.ownHeader = CLIENT
		c.peerHeader = SERVER
	} else {
		c.ownHeader = SERVER
		c.peerHeader = CLIENT
	}

	go c.handleClipboardLoop()

	sigChannel := make(chan os.Signal, 1)
	signal.Notify(sigChannel, os.Interrupt)
	go func() {
		force := false
		for sig := range sigChannel {
			if sig == os.Interrupt {
				if force {
					os.Exit(130)
				}
				force = true
				go c.CloseChannel()
			}
		}
	}()
	return &c, nil
}

func (c *Channel) packet2string(p CBPacket) (string, error) {
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)
	enc := gob.NewEncoder(zw)
	err := enc.Encode(p)
	if err != nil {
		return "", fmt.Errorf("packet2string: cannot encode data: %s", err)
	}
	zw.Close()

	var nonce [24]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return "", fmt.Errorf("packet2string: cannot get random bytes for nonce: %s", err)
	}
	encrypted := secretbox.Seal(nonce[:], b.Bytes(), &nonce, &c.secretKey)

	s := base64.StdEncoding.EncodeToString(encrypted)
	return s, nil
}

func (c *Channel) string2packet(s string) (CBPacket, error) {
	buf, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(buf) < 24 {
		return CBPacket{}, err
	}

	var decryptNonce [24]byte
	copy(decryptNonce[:], buf[:24])
	decrypted, ok := secretbox.Open(nil, buf[24:], &decryptNonce, &c.secretKey)
	if !ok {
		return CBPacket{}, fmt.Errorf("string2packet: cannot decrypt packet")
	}

	rz, err := zlib.NewReader(bytes.NewReader(decrypted))
	if err != nil {
		return CBPacket{}, fmt.Errorf("string2packet: cannot decompress packet: %s", err)
	}
	var p CBPacket
	dec := gob.NewDecoder(rz)
	err = dec.Decode(&p)
	if err != nil {
		return CBPacket{}, fmt.Errorf("string2packet: cannot decode packet: %s", err)
	}
	return p, nil
}

func GetConnData(conn net.Conn, bufferSize int, delay time.Duration) ([]byte, error) {
	buf := make([]byte, bufferSize)
	time.Sleep(delay)
	conn.SetReadDeadline(time.Now().Add(netTimeout))
	length, err := conn.Read(buf)

	if err == nil {
		return buf[0:length], nil
	} else {
		// ignore I/O timeouts etc.
		if nerr, ok := err.(net.Error); ok && (nerr.Temporary() || nerr.Timeout()) {
			return buf[0:0], nil
		}
		return buf[0:0], err
	}
}

func (c *Channel) Receive() []byte {
	cbdata := <-c.receiveChan
	return []byte(cbdata.Payload)
}

func (c *Channel) Send(data []byte) {
	c.sendChan <- CBPacket{Target: c.peerHeader, Payload: string(data)}
}

func (c *Channel) processControlPacket(packet CBPacket) error {
	if packet.Type == PacketTypeControl {
		debugLogger.Println("received cb control data:", packet.Type, packet.Payload)
		args := strings.SplitN(packet.Payload, ":", 2)
		cmd := args[0]
		arg := ""
		if len(args) > 1 {
			arg = args[1]
		}
		switch cmd {
		case "FIN":
			c.sendControl("FIN-ACK")
			c.initiateDelayedShutdown()
		case "FIN-ACK":
			c.shutdown()
		default:
			if c.controlPacketCallback == nil {
				errorLogger.Println("control packet received, but no callback defined")
			}
			c.controlPacketCallback(cmd, arg)
		}
	}
	return nil
}

func (c *Channel) sendControl(msg string) {
	debugLogger.Println("sending control packet:", msg)
	data := []byte(msg)
	c.sendChan <- CBPacket{Target: c.peerHeader, Type: PacketTypeControl, Payload: string(data)}
}

func (c *Channel) CloseChannel() {
	c.sendControl("FIN")
	time.Sleep(8 * c.interval)
	// fail safe if we do not receive a FIN-ACK in time
	c.shutdown()
}

func (c *Channel) initiateDelayedShutdown() {
	c.delayedShutdown.Do(func() {
		go func() {
			debugLogger.Println("trying to tear down channel...")
			time.Sleep(6 * c.interval)
			c.shutdown()
		}()
	})
}

func (c *Channel) shutdown() {
	c.transport.Write("")
	os.Exit(0)
}

func (c *Channel) handleClipboardLoop() {
	var lastRecvIndex = -1
	var lastSendIndex = -1
	var lastAckReceived = -1
	var lastAcked = -1
	var lastRecvTime = time.Now()
	var lastSendTime = time.Now()

	mrand.Seed(time.Now().UnixNano())

	for {
		time.Sleep(c.interval)
		content, err := c.transport.Read()
		// if err != nil {
		// errorLogger.Println("cannot read from transport:", err)
		// goto SkipPacket
		// }
		if content != "" {
			packet, err := c.string2packet(content)
			if err != nil {
				debugLogger.Println("cannot read packet from clipboard:", err)
				goto SkipPacket
			}
			if packet.Target == c.ownHeader {
				idx := packet.Seq
				if packet.Ack > lastAckReceived {
					lastAckReceived = packet.Ack
				}
				if idx == lastRecvIndex+1 {
					lastRecvIndex++
					c.receiveQueue[lastRecvIndex] = packet
					lastRecvTime = time.Now()
					if packet.Type == PacketTypeControl {
						c.processControlPacket(packet)
					} else {
						if packet.Payload != "" {
							c.receiveChan <- packet
						} else {
							select {
							case c.receiveChan <- packet:
							default:
							}
						}
					}
				}
				traceLogger.Printf("\tlastRecvIndex: %d (%s) (%s), lastSendIndex: %d (%s), lastACK: %d\n",
					lastRecvIndex, humanize.Bytes(uint64(len(content))), lastRecvTime.Format("15:04:05"),
					lastSendIndex, lastSendTime.Format("15:04:05"),
					lastAckReceived)
			}
		}
	SkipPacket:

		if lastAckReceived < lastSendIndex {
			if time.Now().Sub(lastSendTime) > 4*c.interval {
				errorLogger.Println("out of sync, trying to resync...")
				// wait for random time to avoid collisions
				time.Sleep(c.interval * time.Duration(mrand.Intn(4)))
				debugLogger.Println("resetting transport")
				c.transport.Reset()
				time.Sleep(3 * c.interval)
				lastSendTime = time.Now()
				pdata, err := c.packet2string(c.sendQueue[lastSendIndex])
				if err == nil {
					err := c.transport.Write(pdata)
					if err != nil {
						errorLogger.Println("cannot write to transport:", err)
					}
				} else {
					errorLogger.Println("cannot send packet:", err)
				}
				continue
			} else {
				debugLogger.Println("last packet not acknowledged, waiting and trying again...")
			}
			continue
		}
		// last packet acked, we may send something new

		var cbdata CBPacket
		var newData bool
		select {
		case cbdata = <-c.sendChan:
			newData = true
		default:
			newData = false
		}
		if !newData {
			if lastAcked < lastRecvIndex {
				debugLogger.Println("acknowledgement outstanding, sending empty packet")
				cbdata = CBPacket{Target: c.peerHeader, Payload: ""}
			} else {
				continue
			}
		}

		lastSendIndex++
		cbdata.Seq = lastSendIndex
		cbdata.Ack = lastRecvIndex
		lastAcked = lastRecvIndex
		c.sendQueue[lastSendIndex] = cbdata
		encoded, err := c.packet2string(cbdata)

		if err == nil {
			err := c.transport.Write(encoded)
			if err != nil {
				errorLogger.Println("cannot write to transport:", err)
			}
		} else {
			errorLogger.Println("cannot send packet:", err)
		}

		lastSendTime = time.Now()

		// clear queue buffer
		if lastRecvIndex >= QueueSize {
			delete(c.receiveQueue, lastRecvIndex-QueueSize)
		}
		if lastSendIndex >= QueueSize {
			delete(c.sendQueue, lastSendIndex-QueueSize)
		}
	}
}
