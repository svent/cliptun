package channel

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os/exec"
	"regexp"
	"strconv"

	shellwords "github.com/mattn/go-shellwords"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/armon/go-socks5"
)

type Tunnel struct {
	Channel
	sshClientConn   *ssh.Client
	socksListenPort int
}

type PortForwarding struct {
	Port     string
	Host     string
	HostPort string
}

type tcpIpForwardPayload struct {
	Addr string
	Port uint32
}

type tcpIpForwardReplyPayload struct {
	Port uint32
}

type forwardedTcpIpPayload struct {
	LAddr string
	LPort uint32
	RAddr string
	RPort uint32
}

var (
	network = "tcp4"
)

func NewTunnel(typ PeerType, options ChannelOptions) (*Tunnel, error) {
	t := &Tunnel{}
	options.ControlPacketCallback = func(cmd, arg string) {
		debugLogger.Printf("control packet callback received: %s (%s)\n", cmd, arg)

		switch cmd {
		case "START-SOCKS":
			t.startSOCKSServer()
		case "SOCKS-AT":
			t.AddLocalPortForwarding(PortForwarding{Port: strconv.Itoa(t.socksListenPort), Host: "localhost", HostPort: arg})
		}

	}
	c, err := NewChannel(typ, options)
	if err != nil {
		return nil, err
	}
	t.Channel = *c
	return t, nil
}

func ParsePortForwarding(param string) (*PortForwarding, error) {
	regex := regexp.MustCompile(`^(\d+):([^:]+):(\d+)$`)
	if !regex.MatchString(param) {
		return nil, fmt.Errorf("Bad port forwarding format: '%s'", param)
	}
	submatches := regex.FindStringSubmatch(param)

	return &PortForwarding{Port: submatches[1], Host: submatches[2], HostPort: submatches[3]}, nil
}

func (t *Tunnel) StartSocksOnPort(port int) {
	t.socksListenPort = port
	t.sendControl("START-SOCKS")
}

func (t *Tunnel) startSOCKSServer() {
	go func() {
		conf := &socks5.Config{Logger: log.New(ioutil.Discard, "", log.LstdFlags)}
		server, err := socks5.New(conf)
		if err != nil {
			errorLogger.Println("cannot create SOCKS server:", err)
			return
		}

		listener, err := net.Listen(network, "localhost:0")
		if err != nil {
			errorLogger.Println("cannot create SOCKS listener:", err)
			return
		}
		_, port, err := net.SplitHostPort(listener.Addr().String())
		if err != nil {
			errorLogger.Println("cannot retrieve SOCKS port:", err)
			return
		}
		t.sendControl("SOCKS-AT:" + port)
		if err := server.Serve(listener); err != nil {
			errorLogger.Println("cannot start SOCKS server:", err)
			return
		}
	}()
}

func (t *Tunnel) handleChannels(chans <-chan ssh.NewChannel) {
	for newChannel := range chans {
		go handleChannel(newChannel)
	}
}

func handleSession(newChannel ssh.NewChannel) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		errorLogger.Printf("Could not accept channel: %s\n", err)
		return
	}

	go func(in <-chan *ssh.Request) {
		defer channel.Close()
		for req := range in {
			debugLogger.Println("session request type:", req.Type, "payload:", string(req.Payload), "want reply:", req.WantReply)
			if req.Type == "subsystem" && len(req.Payload) >= 4 &&
				string(req.Payload[4:]) == "sftp" {
				debugLogger.Printf("request for '%s %s' accepted",
					req.Type, req.Payload)
				req.Reply(true, nil)
			} else if req.Type == "exec" {
				var payload = struct{ Value string }{}
				ssh.Unmarshal(req.Payload, &payload)
				debugLogger.Printf("request for '%s %s' accepted",
					req.Type, payload.Value)

				args, err := shellwords.Parse(payload.Value)
				if err != nil {
					errorLogger.Println("cannot parse command:", err)
					return
				}
				var cmd *exec.Cmd
				if len(args) > 1 {
					cmd = exec.Command(args[0], args[1:]...)
				} else {
					cmd = exec.Command(payload.Value)
				}

				stdout, err := cmd.StdoutPipe()
				if err != nil {
					errorLogger.Println("cannot connect stdout:", err)
					return
				}
				stderr, err := cmd.StderrPipe()
				if err != nil {
					errorLogger.Println("cannot connect stderr:", err)
					return
				}
				input, err := cmd.StdinPipe()
				if err != nil {
					errorLogger.Println("cannot connect stdin:", err)
					return
				}

				if err = cmd.Start(); err != nil {
					errorLogger.Println("cannot start command:", err)
					return
				}

				go io.Copy(input, channel)
				io.Copy(channel, stdout)
				io.Copy(channel.Stderr(), stderr)

				if err = cmd.Wait(); err != nil {
					log.Println("error waiting for command:", err)
					return
				}
				channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				req.Reply(true, nil)
			} else {
				req.Reply(true, nil)
			}
		}
	}(requests)

	serverOptions := []sftp.ServerOption{}
	server, err := sftp.NewServer(
		channel,
		serverOptions...,
	)
	if err != nil {
		errorLogger.Fatalln("cannot create sftp server:", err)
	}
	if err := server.Serve(); err == io.EOF {
		server.Close()
		debugLogger.Println("sftp client closed session")
	} else if err != nil {
		errorLogger.Println("sftp server stopped:", err)
	}

}

func handleChannel(newChannel ssh.NewChannel) {
	debugLogger.Println("received channel:", newChannel.ChannelType())
	t := newChannel.ChannelType()
	if t == "session" {
		handleSession(newChannel)
		return
	}
	if t != "direct-tcpip" {
		errorLogger.Println("received unknown channel type:", t)
		newChannel.Reject(ssh.UnknownChannelType, "unknown channel type: "+t)
		return
	}

	connection, _, err := newChannel.Accept()
	if err != nil {
		errorLogger.Println("cannot not accept channel:", err)
		return
	}
	if connection != nil {
		r := newChannel.ExtraData()
		var cmsg channelOpenDirectMsg
		if err := ssh.Unmarshal(r, &cmsg); err != nil {
			errorLogger.Println("cannot unmarshal OpenDirectMsg data:", err)
			return
		}
		debugLogger.Printf("received connection forward request: %s:%d => %s:%d\n",
			cmsg.Laddr, cmsg.Lport, cmsg.Raddr, cmsg.Rport)

		targetConn, err := net.Dial(network, fmt.Sprintf("%s:%d", cmsg.Raddr, cmsg.Rport))
		if err != nil {
			errorLogger.Println("cannot create forwarding connection:", err)
			return
		}
		go func() {
			io.Copy(connection, targetConn)
			connection.Close()
		}()
		go func() {
			io.Copy(targetConn, connection)
			targetConn.Close()
		}()
	}
}

func (t *Tunnel) handleRequests(in <-chan *ssh.Request, conn *ssh.ServerConn) {
	for req := range in {
		t.handleRequest(req, conn)
	}
}

func (t *Tunnel) handleRequest(req *ssh.Request, conn *ssh.ServerConn) {
	if req.Type != "tcpip-forward" {
		if req.WantReply {
			req.Reply(false, []byte{})
		}
	}
	var msg tcpIpForwardPayload
	if err := ssh.Unmarshal(req.Payload, &msg); err != nil {
		errorLogger.Println("cannot unmarshal IpForwardPayload data:", err)
		return
	}
	debugLogger.Printf("received connection forward request: %s:%d\n",
		msg.Addr, msg.Port)

	bind := fmt.Sprintf("[%s]:%d", msg.Addr, msg.Port)
	debugLogger.Printf("listening on '%s' for incoming connections for remote forwarding\n", bind)
	ln, err := net.Listen(network, bind)
	if err != nil {
		errorLogger.Println("network listen failed:", err)
		return
	}

	reply := tcpIpForwardReplyPayload{msg.Port}
	req.Reply(true, ssh.Marshal(&reply))

	go func() {
		for {
			lconn, err := ln.Accept()
			debugLogger.Println("accepted connection for remote forwarding on", bind)
			if err != nil {
				neterr := err.(net.Error)
				if neterr.Timeout() {
					errorLogger.Printf("accept failed with timeout: %s\n", err)
					continue
				}
				if neterr.Temporary() {
					errorLogger.Printf("accept failed temporary: %s\n", err)
					continue
				}
				break
			}

			go func(lconn net.Conn, laddr string, lport uint32) {
				remotetcpaddr := lconn.RemoteAddr().(*net.TCPAddr)
				raddr := remotetcpaddr.IP.String()
				rport := uint32(remotetcpaddr.Port)

				payload := forwardedTcpIpPayload{laddr, lport, raddr, uint32(rport)}
				mpayload := ssh.Marshal(&payload)

				c, requests, err := conn.OpenChannel("forwarded-tcpip", mpayload)
				if err != nil {
					errorLogger.Println("cannot open channel:", err)
					lconn.Close()
					return
				}
				go ssh.DiscardRequests(requests)

				serve(c, lconn)
			}(lconn, msg.Addr, msg.Port)
		}
	}()
}

func serve(ch io.ReadWriteCloser, conn io.ReadWriteCloser) {
	go func() {
		_, err := io.Copy(ch, conn)
		if err != nil {
			errorLogger.Println("io.Copy failed:", err)
		}
		ch.Close()
	}()

	go func() {
		_, err := io.Copy(conn, ch)
		if err != nil {
			errorLogger.Println("io.Copy failed:", err)
		}
		conn.Close()
	}()

}

func (t *Tunnel) StartClient() {
	s, err := net.Listen(network, "localhost:0")
	if err != nil {
		errorLogger.Fatalln("cannot open listener:", err)
	}
	listenAddr := s.Addr().String()

	go func() {
		defer s.Close()
		conn, _ := s.Accept()
		for {
			data, err := GetConnData(conn, t.bufferSize, t.interval*4/5)
			if err != nil {
				t.CloseChannel()
			} else {
				t.Send(data)
			}
			cbdata := t.Receive()
			conn.Write([]byte(cbdata))

		}
	}()
	debugLogger.Printf("listening for internal ssh traffic on %s\n", listenAddr)

	config := &ssh.ClientConfig{
		User: "cliptun",
		Auth: []ssh.AuthMethod{
			ssh.Password("cliptun"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	t.sshClientConn, err = ssh.Dial(network, listenAddr, config)
	if err != nil {
		errorLogger.Fatalln("ssh.Dial failed:", err)
	}
	debugLogger.Println("ssh connection established")
}

func (t *Tunnel) StartSftp() *sftp.Client {
	client, err := sftp.NewClient(t.sshClientConn)
	if err != nil {
		errorLogger.Println("failed to create sftp client:", err)
	}
	return client
}

func (t *Tunnel) ExecuteCommand(cmd string) (output string, err error) {
	s, err := t.sshClientConn.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()

	out, err := s.CombinedOutput(cmd)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (t *Tunnel) StartServer() {
	addr := t.startSSHServer()

	conn, err := net.Dial(network, addr)
	if err != nil {
		errorLogger.Fatalln("cannot create connection to local SSH server:", err)
	}

	for {
		cbdata := t.Receive()
		conn.Write(cbdata)
		data, err := GetConnData(conn, t.bufferSize, t.interval*4/5)
		if err != nil {
			t.CloseChannel()
		} else {
			t.Send(data)
		}
	}
}

func (t *Tunnel) startSSHServer() string {
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "cliptun" && string(pass) == "cliptun" {
				return &ssh.Permissions{}, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
	}

	rsaPrivateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		errorLogger.Fatalln("failed to generate rsa private key")
	}

	sshPrivateKey, err := ssh.NewSignerFromKey(rsaPrivateKey)
	if err != nil {
		errorLogger.Fatalln("failed to create ssh private key from rsa private key")
	}

	config.AddHostKey(sshPrivateKey)

	listener, err := net.Listen(network, "localhost:0")
	if err != nil {
		errorLogger.Fatalf("failed to create listener on localhost")
	}

	listenAddr := listener.Addr().String()
	debugLogger.Println("started internal SSH server on:", listenAddr)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errorLogger.Fatalln("Failed to accept incoming connection for internal SSH server:", err)
			return
		}

		shConn, chans, reqs, err := ssh.NewServerConn(conn, config)
		if err != nil {
			errorLogger.Fatalln("failed to create SSH server connection:", err)
			return
		}

		go t.handleRequests(reqs, shConn)
		go t.handleChannels(chans)
	}()

	return listenAddr
}

func (c *Tunnel) transfer(localConn net.Conn, target string) {
	sshConn, err := net.Dial("tcp4", target)
	if err != nil {
		errorLogger.Printf("cannot dial for target '%s': %s\n", target, err)
		return
	}
	debugLogger.Printf("connection transferred to '%s'\n", target)

	go serve(sshConn, localConn)
}

func (c *Tunnel) forward(localConn net.Conn, target string) {
	sshConn, err := c.sshClientConn.Dial("tcp4", target)
	if err != nil {
		errorLogger.Printf("cannot dial for taget '%s': %s\n", target, err)
		return
	}
	debugLogger.Printf("connection forwarded to '%s'\n", target)

	go serve(sshConn, localConn)
}

func (c *Tunnel) AddLocalPortForwarding(fwd PortForwarding) {
	go func(fwd PortForwarding) {
		debugLogger.Printf("trying to establish local port fowarding from '%s' to '%s'\n", fwd.Port, fwd.Host+":"+fwd.HostPort)
		localListener, err := net.Listen(network, "localhost:"+fwd.Port)
		if err != nil {
			errorLogger.Printf("net.Listen failed for local port forwarding: %s", err)
			return
		}
		for {
			debugLogger.Println("listening for local connections to forward on", localListener.Addr().String())
			localConn, err := localListener.Accept()
			if err != nil {
				errorLogger.Println("listen.Accept failed:", err)
				continue
			}
			debugLogger.Println("connection accepted on local listener, forwarding...")
			go c.forward(localConn, fwd.Host+":"+fwd.HostPort)
		}
	}(fwd)
}

func (c *Tunnel) AddRemotePortForwarding(fwd PortForwarding) {
	go func(fwd PortForwarding) {
		debugLogger.Printf("trying to listen on port '%s' for remote conns to '%s'\n", fwd.Port, fwd.Host+":"+fwd.HostPort)
		remoteListener, err := c.sshClientConn.Listen(network, "localhost:"+fwd.Port)
		if err != nil {
			errorLogger.Printf("net.Listen failed for remote port forwarding: %s", err)
			return
		}
		for {
			debugLogger.Println("listening for remote conns to forward on", remoteListener.Addr().String())
			remoteConn, err := remoteListener.Accept()
			if err != nil {
				errorLogger.Println("listen.Accept failed:", err)
				continue
			}
			debugLogger.Println("connection accepted on remote listener, forwarding...")
			go c.transfer(remoteConn, fwd.Host+":"+fwd.HostPort)
		}
	}(fwd)
}
