package tailscale

import (
	"net"
	"net/netip"
	"strings"
)

// Adapted from tailscale.com/net/interfaces interfaces.go
//
// Interface returns the current machine's Tailscale interface, if any.
// If none is found, all zero values are returned.
// A non-nil error is only returned on a problem listing the system interfaces.
func Interface() ([]netip.Addr, *net.Interface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, nil, err
	}
	for _, iface := range ifs {
		if !maybeTailscaleInterfaceName(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		var tsIPs []netip.Addr
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				nip, ok := netip.AddrFromSlice(ipnet.IP)
				nip = nip.Unmap()
				if ok && IsTailscaleIP(nip) {
					tsIPs = append(tsIPs, nip)
				}
			}
		}
		if len(tsIPs) > 0 {
			return tsIPs, &iface, nil
		}
	}
	return nil, nil, nil
}

// maybeTailscaleInterfaceName reports whether s is an interface
// name that might be used by Tailscale.
func maybeTailscaleInterfaceName(s string) bool {
	return s == "Tailscale" ||
		strings.HasPrefix(s, "wg") ||
		strings.HasPrefix(s, "ts") ||
		strings.HasPrefix(s, "tailscale") ||
		strings.HasPrefix(s, "utun")
}
