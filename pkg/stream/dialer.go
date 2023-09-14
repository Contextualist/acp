package stream

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/contextualist/acp/pkg/config"
	"github.com/contextualist/acp/pkg/pnet"
)

// A Dialer establishes a stream for sending / receiving files, with the help of a
// rendezvous service.
//
// After implementing a new Dialer:
// 1. Register
//
//	func init() {
//		registerDialer("dialer_name", &DialerImpl{})
//	}
//
// 2. Specify information exchange in pnet.SelfInfo and pnet.PeerInfo
// 3. (optional) Provide configurable options in config.Config
type Dialer interface {
	// Initialize Dialer from config and environment, while checking availability.
	// Return ErrNotAvailable if Dialer is not supported
	Init(conf config.Config) error
	// Populate the info struct to be sent to rendezvous service for information exchange
	SetInfo(info *pnet.SelfInfo)
	// Base on the info received, establish a stream as the sender
	IntoSender(ctx context.Context, info pnet.PeerInfo) (io.WriteCloser, error)
	// Base on the info received, establish a stream as the receiver
	IntoReceiver(ctx context.Context, info pnet.PeerInfo) (io.ReadCloser, error)
}

var allDialers = make(map[string]Dialer)

func registerDialer(name string, d Dialer) {
	allDialers[name] = d
}

func GetDialer(name string) (Dialer, error) {
	d, ok := allDialers[name]
	if !ok {
		return nil, fmt.Errorf("unknown dialer %s", name)
	}
	return d, nil
}

var ErrNotAvailable = errors.New("this dialer is not available")

var defaultLogger pnet.Logger

func SetLogger(l pnet.Logger) {
	defaultLogger = l
}
