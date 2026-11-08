package platform

import "testing"

func TestServerLANAddress(t *testing.T) {
	cases := []struct {
		name, lan, public, cloud, want string
		onCloud                        bool
	}{
		{"home server, public host is the LAN address", "192.168.1.20", "192.168.1.20", "", "192.168.1.20", false},
		{"home server, no public host", "10.0.0.5", "", "", "10.0.0.5", false},
		{"cloud vendor recorded", "172.16.3.4", "", "aliyun", "", false},
		{"public IP as public host", "172.16.3.4", "203.0.113.7", "", "", false},
		{"domain as public host", "192.168.1.20", "nas.example.com", "", "", false},
		{"no private address", "", "", "", "", false},
		{"public address is never a LAN address", "203.0.113.7", "", "", "", false},
		{"IPv6 is not offered", "fd00::1", "", "", "", false},
		// install.sh did not recognize the vendor and recorded the cloud
		// network address as the public host (AWS with IMDSv2 only, GCP,
		// Azure ...): the metadata address answers.
		{"unrecognized cloud, metadata answers", "172.31.5.6", "172.31.5.6", "", "", true},
	}
	for _, c := range cases {
		if got := ServerLANAddress(c.lan, c.public, c.cloud, c.onCloud); got != c.want {
			t.Errorf("%s: ServerLANAddress(%q, %q, %q, %v) = %q, want %q", c.name, c.lan, c.public, c.cloud, c.onCloud, got, c.want)
		}
	}
}
