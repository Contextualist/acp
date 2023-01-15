package pnet

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type (
	lenType uint16

	responseOrError struct {
		*http.Response
		error
	}

	connInfo struct {
		laddr     string
		peerLaddr string
		peerRaddr string
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
	defer sendWriter.Close()
	reqCtx, cancelReqCtx := context.WithCancel(context.Background())
	go func() {
		<-ctx.Done()
		_, _ = sendWriter.Write([]byte{0xff}) // notify early close
		cancelReqCtx()
	}()
	req, err := http.NewRequestWithContext(reqCtx, "POST", bridgeURL, sendReader)
	if err != nil {
		return nil, fmt.Errorf("failed to open a connection to the bridge: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	chRespOrErr := make(chan responseOrError)
	go func() {
		resp, err := client.Do(req)
		if err != nil {
			err = fmt.Errorf("failed to open a connection to the bridge: %w", err)
		}
		chRespOrErr <- responseOrError{resp, err}
	}()
	var laddr string
	select {
	case la := <-client.GetLAddr():
		laddr = la.String()
	case <-ctx.Done():
		return nil, context.Canceled
	}

	err = sendPacket(sendWriter, []byte(fmt.Sprintf("%s|%s", laddr, id)))
	if err != nil {
		return nil, fmt.Errorf("failed to communicate with the bridge: %w", err)
	}
	defaultLogger.Infof("waiting for peer...")
	respOrErr := <-chRespOrErr
	if respOrErr.error != nil {
		return nil, respOrErr.error
	}
	recv, err := receivePacket(respOrErr.Response.Body)
	_ = respOrErr.Response.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to communicate with the bridge: %w", err)
	}
	tmp := strings.Split(string(recv), "|")
	peerLaddr, peerRaddr := tmp[0], tmp[1]
	peerRaddrRe, _ := resolveAddr("tcp", peerRaddr)
	peerRaddr = peerRaddrRe.String()

	return &connInfo{laddr, peerLaddr, peerRaddr}, nil
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
		time.Sleep(dialAttemptInterval)
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
