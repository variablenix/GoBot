package plugins

import "net/netip"

// Shared by all fetchers of user-controlled URLs, not only website checks.
// Local, shared, documentation, benchmark, reserved, and transition address
// space must never be reachable through a public IRC lookup command.
var nonPublicIPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var internetIPv6Global = netip.MustParsePrefix("2000::/3")

func publicInternetIP(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Is6() && !internetIPv6Global.Contains(ip) {
		return false
	}
	for _, prefix := range nonPublicIPPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
