package platform

import (
	"net"
	"net/netip"
	"strings"
	"time"
)

var sleep = func(seconds int) { time.Sleep(time.Duration(seconds) * time.Second) }

// LANAddress returns this machine's address on the local network: the source
// address the OS would use to reach a public IP (no packet is sent; UDP
// "connect" only picks a route), falling back to the first private IPv4 of an
// interface that is up. Addresses of VPN, proxy and virtual adapters are
// skipped (a TUN proxy such as sing-box often owns the default route; its
// address is not reachable from the LAN). Empty when there is no network.
func LANAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	owner := map[string]string{} // address -> interface name
	for _, ifc := range ifaces {
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				owner[n.IP.String()] = ifc.Name
			}
		}
	}
	if c, err := net.Dial("udp4", "223.5.5.5:53"); err == nil {
		ip := c.LocalAddr().(*net.UDPAddr).IP
		_ = c.Close()
		if lanCandidate(ip, owner[ip.String()]) {
			return ip.String()
		}
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if ip4 := n.IP.To4(); ip4 != nil && lanCandidate(ip4, ifc.Name) {
					return ip4.String()
				}
			}
		}
	}
	return ""
}

// virtualIfacePatterns are name fragments of VPN, proxy and virtual adapters.
var virtualIfacePatterns = []string{"tun", "tap", "utun", "wg", "wireguard", "sing-box", "clash", "mihomo", "vpn",
	"tailscale", "zerotier", "veth", "docker", "br-", "virbr", "vethernet", "wsl", "hyper-v", "vmware", "virtualbox", "vboxnet", "loopback"}

// lanCandidate reports whether ip on interface name can be a LAN address.
func lanCandidate(ip net.IP, name string) bool {
	ip4 := ip.To4()
	if ip4 == nil || !ip4.IsPrivate() {
		return false
	}
	a, _ := netip.AddrFromSlice(ip4)
	for _, p := range []string{"198.18.0.0/15", "100.64.0.0/10"} {
		if netip.MustParsePrefix(p).Contains(a) {
			return false
		}
	}
	n := strings.ToLower(name)
	for _, v := range virtualIfacePatterns {
		if strings.Contains(n, v) {
			return false
		}
	}
	return true
}
