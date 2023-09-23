package tailscale

import (
	"net/netip"
	"sync"
)

// Adapted from tailscale.com/net/tsaddr tsaddr.go
//
// IsTailscaleIP reports whether ip is an IP address in a range that
// Tailscale assigns from.
func IsTailscaleIP(ip netip.Addr) bool {
	if ip.Is4() {
		return CGNATRange().Contains(ip) && !ChromeOSVMRange().Contains(ip)
	}
	return TailscaleULARange().Contains(ip)
}

var (
	// CGNATRange returns the Carrier Grade NAT address range that
	// is the superset range that Tailscale assigns out of.
	// See https://tailscale.com/s/cgnat
	// Note that Tailscale does not assign out of the ChromeOSVMRange.
	CGNATRange = mustPrefix("100.64.0.0/10")
	// ChromeOSVMRange returns the subset of the CGNAT IPv4 range used by
	// ChromeOS to interconnect the host OS to containers and VMs. We
	// avoid allocating Tailscale IPs from it, to avoid conflicts.
	ChromeOSVMRange = mustPrefix("100.115.92.0/23")
	// TailscaleULARange returns the IPv6 Unique Local Address range that
	// is the superset range that Tailscale assigns out of.
	TailscaleULARange = mustPrefix("fd7a:115c:a1e0::/48")
)

func mustPrefix(prefix string) func() *netip.Prefix {
	return sync.OnceValue(func() *netip.Prefix {
		v, err := netip.ParsePrefix(prefix)
		if err != nil {
			panic(err)
		}
		return &v
	})
}
