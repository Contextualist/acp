package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fsnotify/fsnotify"

	"github.com/contextualist/acp/pkg/config"
	"github.com/contextualist/acp/pkg/pnet"
	tsapi "github.com/contextualist/acp/pkg/tailscale"
)

func init() {
	registerDialer("tailscale", &Tailscale{tun: &tailscaleTun{}, tdrop: &taildrop{}})
}

type TSCapability uint

const (
	TSTaildrop TSCapability = 1 << iota
	TSTun
)

type Tailscale struct {
	tun        *tailscaleTun
	tdrop      *taildrop
	capability TSCapability
}

func (d *Tailscale) Init(conf config.Config) error {
	if d.tun.Init(conf) == nil {
		d.capability |= TSTun
	}
	if d.tdrop.Init(conf) == nil {
		d.capability |= TSTaildrop
	}
	if d.capability == 0 {
		return ErrNotAvailable
	}
	return nil
}

func (d *Tailscale) SetInfo(info *pnet.SelfInfo) {
	if d.capability&TSTun != 0 {
		d.tun.SetInfo(info)
	} else {
		d.tdrop.SetInfo(info)
	}
	info.TSCap = uint(d.capability)
}

func (d *Tailscale) IntoSender(ctx context.Context, info pnet.PeerInfo) (io.WriteCloser, error) {
	inner, err := d.pickImpl(d.capability, TSCapability(info.TSCap))
	if err != nil {
		return nil, err
	}
	return inner.IntoSender(ctx, info)
}

func (d *Tailscale) IntoReceiver(ctx context.Context, info pnet.PeerInfo) (io.ReadCloser, error) {
	inner, err := d.pickImpl(TSCapability(info.TSCap), d.capability)
	if err != nil {
		return nil, err
	}
	return inner.IntoReceiver(ctx, info)
}

func (d *Tailscale) pickImpl(cap1, cap2 TSCapability) (Dialer, error) {
	cap := cap1 & cap2
	if cap&TSTun != 0 {
		defaultLogger.Debugf("using Tailscale Tun")
		return d.tun, nil
	}
	if cap&TSTaildrop != 0 {
		defaultLogger.Debugf("using Taildrop")
		return d.tdrop, nil
	}
	return nil, errors.New("neither of Tailscale Tun and Taildrop is supported on both side")
}

type tailscaleTun struct {
	laddr string
}

func (d *tailscaleTun) Init(conf config.Config) error {
	addrs, _, err := tsapi.Interface()
	if err != nil || len(addrs) == 0 {
		defaultLogger.Debugf("tailscale network interface search failed, found addrs %v: %v", addrs, err)
		return ErrNotAvailable
	}
	laddr := fmt.Sprintf("%s:%v", addrs[0].String(), conf.Ports[0])
	listener, err := pnet.Listen(context.TODO(), "tcp", laddr)
	if err != nil {
		defaultLogger.Debugf("listen at tailscale addr %s failed: %v", laddr, err)
		return ErrNotAvailable
	}
	d.laddr = listener.Addr().String()
	_ = listener.Close()
	defaultLogger.Debugf("tailscale IP address is available")
	return nil
}

func (d *tailscaleTun) SetInfo(info *pnet.SelfInfo) {
	info.TSAddr = d.laddr
}

func (d *tailscaleTun) IntoSender(ctx context.Context, info pnet.PeerInfo) (io.WriteCloser, error) {
	return pnet.RendezvousWithTimeout(ctx, d.laddr, []pnet.AddrPair{{PriAddr: info.TSAddr, PubAddr: info.TSAddr}})
}

func (d *tailscaleTun) IntoReceiver(ctx context.Context, info pnet.PeerInfo) (io.ReadCloser, error) {
	return pnet.RendezvousWithTimeout(ctx, d.laddr, []pnet.AddrPair{{PriAddr: info.TSAddr, PubAddr: info.TSAddr}})
}

type taildrop struct {
	cli  *tsapi.TSCli
	tsIP string
}

func (d *taildrop) Init(_ config.Config) error {
	bin, err := tsapi.Path()
	if err != nil {
		defaultLogger.Debugf("not found: %v", err)
		return ErrNotAvailable
	}
	d.cli = &tsapi.TSCli{Prefix: []string{bin}}

	if soc, ok := os.LookupEnv("TS_SOCKET"); ok {
		defaultLogger.Debugf("will be using socket %s when running tailscale", soc)
		d.cli.Prefix = append(d.cli.Prefix, fmt.Sprintf("--socket=%s", soc))
	}

	sf, err := d.cli.RunStatus(context.TODO())
	if err != nil {
		defaultLogger.Debugf(err.Error())
		return ErrNotAvailable
	}
	d.tsIP = (*sf.TsIPs)[0]
	if runtime.GOOS == "linux" && *sf.Tun {
		// heuristic test if tailscaled is running as root
		defaultLogger.Debugf("will be running tailscale as root")
		d.cli.Prefix = append([]string{"sudo"}, d.cli.Prefix...)
	}

	defaultLogger.Debugf("taildrop is available")
	return nil
}

func (d *taildrop) SetInfo(info *pnet.SelfInfo) {
	info.TSAddr = d.tsIP
}

const TmpArcName = "acp-tmp.tar.gz"

func (d *taildrop) IntoSender(ctx context.Context, info pnet.PeerInfo) (io.WriteCloser, error) {
	peerIP := stripPort(info.TSAddr)
	return d.cli.StartCp(ctx, TmpArcName, peerIP, defaultLogger.Infof)
}

func (d *taildrop) IntoReceiver(ctx context.Context, info pnet.PeerInfo) (r io.ReadCloser, err error) {
	var tsInbox string
	switch runtime.GOOS {
	case "linux":
		tsInbox, err = os.Getwd()
	case "darwin", "windows":
		var home string
		home, err = os.UserHomeDir()
		tsInbox = filepath.Join(home, "Downloads")
	default:
		return nil, fmt.Errorf("OS %s is not supported", runtime.GOOS)
	}
	if err != nil {
		return nil, fmt.Errorf("error getting Taildrop receive dir: %w", err)
	}
	targetName := filepath.Join(tsInbox, TmpArcName)

	defaultLogger.Infof("Taildrop receiving...")
	if runtime.GOOS == "linux" {
		out, err := d.cli.RunGet(ctx, ".")
		if err != nil {
			return nil, fmt.Errorf("error running tailscale file get: %w\noutput:\n%s", err, string(out))
		}
	} else {
		// The tailscale daemon automatically receives the file to Downloads, so we wait for it to finish
		// CAVEAT: if `targetName` already exist, tailscale daemon will receive file as `targetName 1`
		err := waitFileTransferEnd(targetName)
		if err != nil {
			return nil, fmt.Errorf("error waiting for Taildrop receive: %w", err)
		}
	}
	fi, err := os.Open(targetName)
	if err != nil {
		return nil, err
	}
	return &fileWithCleanup{fi}, nil
}

func stripPort(s string) string {
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return s
	}
	return ap.Addr().String()
}

func waitFileTransferEnd(fname string) (err error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("cannot create file watcher: %w", err)
	}
	defer watcher.Close()
	parent := path.Dir(fname)
	err = watcher.Add(parent)
	if err != nil {
		return fmt.Errorf("cannot subscribe watcher to path %s: %w", parent, err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !strings.HasPrefix(event.Name, fname) {
				continue
			}
			defaultLogger.Debugf("fs event: %v", event)
			if event.Has(fsnotify.Create) && event.Name == fname {
				return
			}
		case err = <-watcher.Errors:
			return
		}
	}
}

type fileWithCleanup struct {
	*os.File
}

func (f *fileWithCleanup) Close() error {
	_ = os.Remove(f.File.Name())
	return f.File.Close()
}
