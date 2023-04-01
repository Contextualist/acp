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

	// Options for function HolePunching
	HolePunchingOptions struct {
		// Whether to use IPv6 instead of IPv4 for rendezvous
		UseIPv6 bool
		// Local port(s) to be used for rendezvous; default (nil) will be interpreted as {0}
		Ports []int
		// Whether to try requesting UPnP port mapping from the router
		UPnP bool
	}
)

type (
	lenType uint16

	readerOrError struct {
		io.ReadCloser
		error
	}

	connInfo struct {
		laddr     string
		peerAddrs []string
		peerNPlan int
	}

	selfInfo struct {
		PriAddr  string `json:"priAddr"`
		ChanName string `json:"chanName"`
		NPlan    int    `json:"nPlan"`
	}
	addrPair struct {
		PriAddr string `json:"priAddr"`
		PubAddr string `json:"pubAddr"`
	}
	peerInfo struct {
		PeerAddrs []addrPair `json:"peerAddrs"`
		PeerNPlan int        `json:"peerNPlan"`
	}
)

const (
	dialAttemptInterval = 300 * time.Millisecond
	rendezvousTimeout   = 1600 * time.Millisecond
)

var defaultLogger Logger

// HolePunching negotiates via a rendezvous server with a peer with the same id to establish a connection.
func HolePunching(ctx context.Context, bridgeURL string, id string, isA bool, opts HolePunchingOptions, l Logger) (conn net.Conn, err error) {
	defaultLogger = l
	if len(opts.Ports) == 0 {
		opts.Ports = []int{0}
	}
	if opts.UPnP {
		err := addPortMapping(ctx, opts.Ports...)
		if err != nil {
			defaultLogger.Infof("failed to add port mapping: %v", err)
		}
	}

	nplan := len(opts.Ports)
	nA := 2
	nB := 2
	*tern(isA, &nA, &nB) = nplan
	// Try out all nA x nB plans
	for i := 0; i < nA; i++ {
		for j := 0; j < nB; j++ {
			q := tern(isA, i, j)
			info, err := exchangeConnInfo(ctx, bridgeURL, id, opts.Ports[q], nplan, opts.UseIPv6)
			if err != nil {
				return nil, err
			}
			*tern(isA, &nB, &nA) = info.peerNPlan
			ctx1, cancel := context.WithTimeout(ctx, rendezvousTimeout)
			defer cancel()
			conn, err := rendezvous(ctx1, info)
			if err != nil {
				if errors.Is(ctx1.Err(), context.DeadlineExceeded) {
					defaultLogger.Infof("rendezvous timeout for %+v", info)
					continue
				}
				return nil, err
			}
			return conn, nil
		}
	}
	return nil, errors.New("all rendezvous attempts failed")
}

func exchangeConnInfo(ctx context.Context, bridgeURL string, id string, port int, nplan int, useIPv6 bool) (*connInfo, error) {
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
	info := selfInfo{ChanName: id, NPlan: nplan}
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

	return exchangeConnInfoProto(ctx, sendWriter, chRecvOrErr, &info, cancelReq)
}

func exchangeConnInfoProto(ctx context.Context, sender io.WriteCloser, chRecvOrErr <-chan readerOrError, sinfo *selfInfo, cancelReq context.CancelFunc) (*connInfo, error) {
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
	var pinfo peerInfo
	err = json.Unmarshal(recv, &pinfo)
	if err != nil {
		return nil, fmt.Errorf("failed to parse msg from bridge: %w", err)
	}

	var addrs []string
	for _, ap := range pinfo.PeerAddrs {
		addrs = append(addrs, ap.PriAddr)
		if ap.PubAddr != ap.PriAddr {
			addrs = append(addrs, ap.PubAddr)
		}
	}
	return &connInfo{sinfo.PriAddr, addrs, pinfo.PeerNPlan}, nil
}

func rendezvous(ctx context.Context, info *connInfo) (conn net.Conn, err error) {
	defaultLogger.Infof("rendezvous with %s", strings.Join(info.peerAddrs, " | "))
	chWin := make(chan net.Conn)
	l, err := Listen(ctx, "tcp", info.laddr)
	if err != nil {
		return nil, fmt.Errorf("unable to set up rendezvous: %w", err)
	}
	defer l.Close()
	cc := make(chan struct{})
	defer close(cc)
	go accept(ctx, l, chWin, cc)
	for _, peerAddr := range info.peerAddrs {
		go connect(ctx, info.laddr, peerAddr, chWin, cc)
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

func tern[T any](t bool, a T, b T) T {
	if t {
		return a
	}
	return b
}
