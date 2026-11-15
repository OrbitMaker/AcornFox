package runner

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// hostActivity is the pull watch's second signal (pull_fallback.go): a
// counter that grows with the bytes this machine receives over the network
// and reads or writes on its disks. ok is false where it cannot be read
// (outside Linux). Tests replace it.
//
// Why it is needed (measured 2026-11, Docker 29 with the containerd image
// store): when a pull continues an image that an earlier, stopped pull left
// partly downloaded, Docker can send no progress at all until the image is
// there ("Pulling from …", then nothing, then "Digest: …"), while the layer
// is downloading the whole time. It happens when the image index carries an
// attestation manifest that the earlier pull already fetched completely:
// Docker then hides the progress of every layer of the pull. The runner is
// not allowed to look into Docker's or containerd's own directories, so it
// watches the machine's network and disk counters instead. Other traffic on
// the machine can only make a stopped pull look alive (it is then waited for
// longer, up to the pull's total limit); it cannot make a moving pull look
// stopped.
var hostActivity = readHostActivity

// Files read by readHostActivity (variables so tests can point elsewhere).
var (
	procNetDev    = "/proc/net/dev"
	procDiskStats = "/proc/diskstats"
)

func readHostActivity() (int64, bool) {
	rx, ok1 := netRxBytes(procNetDev)
	disk, ok2 := diskBytes(procDiskStats)
	return rx + disk, ok1 || ok2
}

// netRxBytes sums the received bytes of every interface in /proc/net/dev
// (loopback included: a registry mirror on this machine is reached over it).
func netRxBytes(path string) (int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var sum int64
	ok := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		_, rest, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 1 {
			continue
		}
		if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
			sum += n
			ok = true
		}
	}
	return sum, ok
}

// diskBytes sums sectors read and written (fields 6 and 10, 512 bytes each)
// over the lines of /proc/diskstats. Partitions and the disks they are on
// are both counted; the counter is only compared with itself.
func diskBytes(path string) (int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var sum int64
	ok := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}
		r, err1 := strconv.ParseInt(fields[5], 10, 64)
		w, err2 := strconv.ParseInt(fields[9], 10, 64)
		if err1 == nil && err2 == nil {
			sum += (r + w) * 512
			ok = true
		}
	}
	return sum, ok
}
