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
	"time"
)

type (
	lenType uint16

	readerOrError struct {
		io.ReadCloser
		error
	}

	connInfo struct {
		laddr     string
		peerLaddr string
		peerRaddr string
	}

	selfInfo struct {
		PriAddr  string `json:"priAddr"`
		ChanName string `json:"chanName"`
	}
	peerInfo struct {
		PriAddr string `json:"priAddr"`
		PubAddr string `json:"pubAddr"`
	}
)

const (
	dialAttemptInterval = 500 * time.Millisecond
	rendezvousTimeout   = 5 * time.Second
)

// Logger interface accepted by pnet for internal logging
type Logger interface {
	Infof(format string, a ...any)
	Debugf(format string, a ...any)
}

var defaultLogger Logger

// HolePunching negotiates via a rendezvous server with a peer with the same id to establish a connection.
func HolePunching(ctx context.Context, bridgeURL string, id string, useIPv6 bool, l Logger) (conn net.Conn, err error) {
	defaultLogger = l
	info, err := exchangeConnInfo(ctx, bridgeURL, id, useIPv6)
	if err != nil {
		return nil, err
	}
	return rendezvous(ctx, info)
}

func exchangeConnInfo(ctx context.Context, bridgeURL string, id string, useIPv6 bool) (*connInfo, error) {
	client := NewHTTPClient(useIPv6)
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
	info := selfInfo{ChanName: id}
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
	var pinfo peerInfo
	err = json.Unmarshal(recv, &pinfo)
	if err != nil {
		return nil, fmt.Errorf("failed to parse msg from bridge: %w", err)
	}

	return &connInfo{sinfo.PriAddr, pinfo.PriAddr, pinfo.PubAddr}, nil
}

func rendezvous(ctx context.Context, info *connInfo) (conn net.Conn, err error) {
	defaultLogger.Infof("rendezvous with %s | %s", info.peerLaddr, info.peerRaddr)
	chWin := make(chan net.Conn)
	l, err := Listen(ctx, "tcp", info.laddr)
	if err != nil {
		return nil, fmt.Errorf("unable to set up rendezvous: %w", err)
	}
	defer l.Close()
	cc := make(chan struct{})
	defer close(cc)
	go accept(ctx, l, chWin, cc)
	go connect(ctx, info.laddr, info.peerLaddr, chWin, cc)
	if info.peerRaddr != info.peerLaddr {
		go connect(ctx, info.laddr, info.peerRaddr, chWin, cc)
	}

	select {
	case conn = <-chWin:
	case <-time.After(rendezvousTimeout):
		return nil, fmt.Errorf("rendezvous timeout for %+v", info)
	case <-ctx.Done():
		return nil, context.Canceled
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
