package pnet

import (
	"context"
	"net"
	"net/http"
)

type HTTPClient struct {
	*http.Client
	chLaddr chan net.Addr
}

func NewHTTPClient(useIpv6 bool) *HTTPClient {
	client := &HTTPClient{
		Client:  &http.Client{},
		chLaddr: make(chan net.Addr, 1),
	}

	tr := *(http.DefaultTransport.(*http.Transport)) // create a value copy of the default
	tr.DialContext = func(ctx context.Context, _network, addr string) (net.Conn, error) {
		network := "tcp4"
		if useIpv6 {
			network = "tcp6"
		}
		c, err := DialContext(ctx, network, ":0", addr)
		if err == nil {
			client.chLaddr <- c.LocalAddr()
		} else {
			defaultLogger.Debugf("pnet.HTTPClient.Transport.DialContext: %v", err)
			client.chLaddr <- &net.TCPAddr{}
		}
		return c, err
	}
	client.Client.Transport = &tr
	return client
}

func (cl *HTTPClient) GetLAddr() <-chan net.Addr {
	return cl.chLaddr
}
