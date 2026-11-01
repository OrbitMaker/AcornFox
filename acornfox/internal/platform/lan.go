package platform

import (
	"net"
	"time"
)

var sleep = func(seconds int) { time.Sleep(time.Duration(seconds) * time.Second) }

// LANAddress returns this machine's address on the local network: the source
// address the OS would use to reach a public IP (no packet is sent; UDP
// "connect" only picks a route), falling back to the first private IPv4 of an
// interface that is up. Empty when there is no network.
func LANAddress() string {
	if c, err := net.Dial("udp4", "223.5.5.5:53"); err == nil {
		ip := c.LocalAddr().(*net.UDPAddr).IP
		_ = c.Close()
		if ip.IsPrivate() {
			return ip.String()
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if ip4 := n.IP.To4(); ip4 != nil && ip4.IsPrivate() {
					return ip4.String()
				}
			}
		}
	}
	return ""
}
