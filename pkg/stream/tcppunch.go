package stream

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/contextualist/acp/pkg/config"
	"github.com/contextualist/acp/pkg/pnet"
)

func init() {
	registerDialer("tcp_punch", &TcpHolePunch{})
}

type TcpHolePunch struct {
	bridgeURL string
	id        string
	psk       []byte
	// Whether to use IPv6 instead of IPv4 for rendezvous
	useIPv6 bool
	// Local port(s) to be used for rendezvous
	ports []int
	// Whether to try requesting uPnP port mapping from the router
	uPnP bool
}

func (d *TcpHolePunch) Init(conf config.Config) (err error) {
	d.bridgeURL = conf.Server + "/v2/exchange"
	d.id = conf.ID
	d.psk, err = base64.StdEncoding.DecodeString(conf.PSK)
	if err != nil {
		return fmt.Errorf("error decoding PSK: %w", err)
	}
	d.useIPv6 = conf.UseIPv6
	d.ports = conf.Ports
	d.uPnP = conf.UPnP
	return nil
}

func (d *TcpHolePunch) SetInfo(info *pnet.SelfInfo) {
	info.NPlan = len(d.ports)
}

func (d *TcpHolePunch) IntoSender(ctx context.Context, info pnet.PeerInfo) (io.WriteCloser, error) {
	conn, err := d.holePunching(ctx, info, true)
	if err != nil {
		return nil, err
	}
	return encrypted(conn, d.psk)
}

func (d *TcpHolePunch) IntoReceiver(ctx context.Context, info pnet.PeerInfo) (io.ReadCloser, error) {
	conn, err := d.holePunching(ctx, info, false)
	if err != nil {
		return nil, err
	}
	return encrypted(conn, d.psk)
}

// HolePunching negotiates via a rendezvous server with a peer with the same id to establish a connection.
func (d *TcpHolePunch) holePunching(ctx context.Context, info pnet.PeerInfo, isA bool) (conn net.Conn, err error) {
	if d.uPnP {
		err = pnet.AddPortMapping(ctx, d.ports...)
		if err != nil {
			defaultLogger.Infof("failed to add port mapping: %v", err)
		}
	}

	nplan := len(d.ports)
	if conn, err = pnet.RendezvousWithTimeout(ctx, info.Laddr, info.PeerAddrs); err == nil {
		return conn, nil
	}

	// Try out the rest of nA x nB plans
	var planp []int
	for i := 0; i < tern(isA, nplan, info.PeerNPlan); i++ {
		for j := 0; j < tern(isA, info.PeerNPlan, nplan); j++ {
			planp = append(planp, d.ports[tern(isA, i, j)])
		}
	}
	for q := range planp[1:] {
		info, err := pnet.ExchangeConnInfo(ctx, d.bridgeURL, &pnet.SelfInfo{ChanName: d.id, NPlan: nplan}, q, d.useIPv6)
		if err != nil {
			return nil, err
		}
		if conn, err = pnet.RendezvousWithTimeout(ctx, info.Laddr, info.PeerAddrs); err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("all rendezvous attempts failed")
}

func tern[T any](t bool, a T, b T) T {
	if t {
		return a
	}
	return b
}
