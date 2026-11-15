package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostActivityFiles(t *testing.T) {
	dir := t.TempDir()
	net := filepath.Join(dir, "dev")
	disk := filepath.Join(dir, "diskstats")
	_ = os.WriteFile(net, []byte(`Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:    1000      10    0    0    0     0          0         0     1000      10    0    0    0     0       0          0
  eth0: 2000000    2000    0    0    0     0          0         0   500000     900    0    0    0     0       0          0
`), 0o644)
	_ = os.WriteFile(disk, []byte(` 253       0 vda 100 0 2048 50 200 0 4096 70 0 120 120 0 0 0 0
 253       1 vda1 90 0 2000 40 190 0 4000 60 0 100 100 0 0 0 0
`), 0o644)
	if v, ok := netRxBytes(net); !ok || v != 2001000 {
		t.Fatalf("net rx %d %v", v, ok)
	}
	if v, ok := diskBytes(disk); !ok || v != (2048+4096+2000+4000)*512 {
		t.Fatalf("disk %d %v", v, ok)
	}
	oldN, oldD := procNetDev, procDiskStats
	t.Cleanup(func() { procNetDev, procDiskStats = oldN, oldD })
	procNetDev, procDiskStats = net, filepath.Join(dir, "missing")
	if v, ok := readHostActivity(); !ok || v != 2001000 {
		t.Fatalf("one file missing: %d %v", v, ok)
	}
	procNetDev = filepath.Join(dir, "missing")
	if _, ok := readHostActivity(); ok {
		t.Fatal("both missing must report not ok")
	}
}
