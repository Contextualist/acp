package pnet

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type (
	// Logger interface accepted by pnet for internal logging
	Logger interface {
		Infof(format string, a ...any)
		Debugf(format string, a ...any)
	}
)

type (
	lenType uint16

	readerOrError struct {
		io.ReadCloser
		error
	}

	SelfInfo struct {
		PriAddr  string `json:"priAddr"`
		ChanName string `json:"chanName"`
		NPlan    int    `json:"nPlan,omitempty"`
		TSAddr   string `json:"tsAddr,omitempty"`
		TSCap    uint   `json:"tsCap,omitempty"`
	}
	AddrPair struct {
		PriAddr string `json:"priAddr"`
		PubAddr string `json:"pubAddr"`
	}
	PeerInfo struct {
		Laddr     string
		PeerAddrs []AddrPair `json:"peerAddrs"`
		PeerNPlan int        `json:"peerNPlan,omitempty"`
		TSAddr    string     `json:"tsAddr,omitempty"`
		TSCap     uint       `json:"tsCap,omitempty"`
	}
)

const (
	dialAttemptInterval = 300 * time.Millisecond
	rendezvousTimeout   = 1600 * time.Millisecond
)

var defaultLogger Logger

// SetLogger sets the internal logger for pnet
func SetLogger(l Logger) {
	defaultLogger = l
}

// ExchangeConnInfo exchanges oneself's info for the peer's info, which can be used to establish a connection
func ExchangeConnInfo(ctx context.Context, bridgeURL string, info *SelfInfo, port int, useIPv6 bool) (*PeerInfo, error) {
	client := NewHTTPClient(useIPv6, fmt.Sprintf(":%v", port))
	sendReader, sendWriter := io.Pipe()
	reqCtx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()
	req, err := http.NewRequestWithContext(reqCtx, "POST", bridgeURL, sendReader)
	if err != nil {
		return nil, fmt.Errorf("failed to open a connection to the bridge: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	chRecvOrErr := make(chan readerOrError)
	go func() {
		resp, err := client.Do(req)
		if err != nil {
			err = fmt.Errorf("failed to open a connection to the bridge: %w", err)
			chRecvOrErr <- readerOrError{nil, err}
		} else {
			chRecvOrErr <- readerOrError{resp.Body, nil}
		}
		close(chRecvOrErr)
	}()
	select {
	case la, ok := <-client.GetLAddr():
		if !ok { // dial error
			recvOrErr := <-chRecvOrErr
			if recvOrErr.error != nil {
				return nil, recvOrErr.error
			}
			return nil, errors.New("internal error: dial failed but HTTP request finished silently")
		}
		info.PriAddr = la.String()
	case <-ctx.Done():
		return nil, context.Canceled
	}

	return exchangeConnInfoProto(ctx, sendWriter, chRecvOrErr, info, cancelReq)
}

func exchangeConnInfoProto(ctx context.Context, sender io.WriteCloser, chRecvOrErr <-chan readerOrError, sinfo *SelfInfo, cancelReq context.CancelFunc) (*PeerInfo, error) {
	infoEnc, _ := json.Marshal(sinfo)
	err := sendPacket(sender, infoEnc)
	if err != nil {
		return nil, fmt.Errorf("failed to communicate with the bridge: %w", err)
	}
	defaultLogger.Debugf("send %s", infoEnc)

	if ctx.Done() != nil {
		defer sender.Close()
	} else {
		sender.Close()
	}

	defaultLogger.Infof("waiting for peer...")
	var recvOrErr readerOrError
	select {
	case recvOrErr = <-chRecvOrErr:
	case <-ctx.Done():
		_, _ = sender.Write([]byte{0xff}) // notify early close
		cancelReq()
		return nil, context.Canceled
	}
	if recvOrErr.error != nil {
		return nil, recvOrErr.error
	}
	recv, err := receivePacket(recvOrErr.ReadCloser)
	_ = recvOrErr.ReadCloser.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to communicate with the bridge: %w", err)
	}
	defaultLogger.Debugf("recv %s", recv)
	var pinfo PeerInfo
	err = json.Unmarshal(recv, &pinfo)
	if err != nil {
		return nil, fmt.Errorf("failed to parse msg from bridge: %w", err)
	}

	pinfo.Laddr = sinfo.PriAddr
	return &pinfo, nil
}

func flattenAddrs(aps []AddrPair) []string {
	var addrs []string
	for _, ap := range aps {
		addrs = append(addrs, ap.PriAddr)
		if ap.PubAddr != ap.PriAddr {
			addrs = append(addrs, ap.PubAddr)
		}
	}
	return addrs
}

// RendezvousWithTimeout performs simultaneous connection opening for TCP hole punching, with `rendezvousTimeout`
func RendezvousWithTimeout(ctx context.Context, laddr string, peerAddrs []AddrPair) (conn net.Conn, err error) {
	ctx1, cancel := context.WithTimeout(ctx, rendezvousTimeout)
	defer cancel()
	conn, err = rendezvous(ctx1, laddr, flattenAddrs(peerAddrs))
	if err != nil {
		if errors.Is(ctx1.Err(), context.DeadlineExceeded) {
			defaultLogger.Infof("rendezvous timeout for %v -> %v", laddr, peerAddrs)
		}
		return nil, err
	}
	return conn, nil
}

func rendezvous(ctx context.Context, laddr string, peerAddrs []string) (conn net.Conn, err error) {
	defaultLogger.Infof("rendezvous with %s", strings.Join(peerAddrs, " | "))
	chWin := make(chan net.Conn)
	l, err := Listen(ctx, "tcp", laddr)
	if err != nil {
		return nil, fmt.Errorf("unable to set up rendezvous: %w", err)
	}
	defer l.Close()
	cc := make(chan struct{})
	defer close(cc)
	go accept(ctx, l, chWin, cc)
	for _, peerAddr := range peerAddrs {
		go connect(ctx, laddr, peerAddr, chWin, cc)
	}

	select {
	case conn = <-chWin:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return conn, nil
}

func sendPacket(conn io.Writer, data []byte) (err error) {
	if err = binary.Write(conn, binary.BigEndian, lenType(len(data))); err != nil {
		return
	}
	if _, err = conn.Write(data); err != nil {
		return
	}
	return
}

func receivePacket(conn io.Reader) (data []byte, err error) {
	var plen lenType
	if err = binary.Read(conn, binary.BigEndian, &plen); err != nil {
		return
	}
	buf := make([]byte, plen)
	if _, err = io.ReadFull(conn, buf); err != nil {
		return
	}
	return buf, nil
}

func accept(ctx context.Context, l net.Listener, chWin chan<- net.Conn, cc <-chan struct{}) {
	conn, err := l.Accept()
	if err != nil {
		return
	}
	select {
	case chWin <- conn:
		defaultLogger.Debugf("accepted %v", conn.LocalAddr())
	case <-cc:
		conn.Close()
	case <-ctx.Done():
		conn.Close()
	}
}

func connect(ctx context.Context, laddr, raddr string, chWin chan<- net.Conn, cc <-chan struct{}) {
	var conn net.Conn
	var err error
	for {
		select {
		case <-cc:
			return
		default:
		}
		conn, err = DialContext(ctx, "tcp", laddr, raddr)
		if err == nil {
			break
		}
		select {
		case <-time.After(dialAttemptInterval):
		case <-ctx.Done():
			return
		}
	}
	select {
	case chWin <- conn:
		defaultLogger.Debugf("connected %v->%v", laddr, raddr)
	case <-cc:
		conn.Close()
	case <-ctx.Done():
		conn.Close()
	}
}
