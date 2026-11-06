package runner

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"unicode/utf8"
)

// tailBuffer is a fixed-size byte ring that keeps the last max bytes written
// to it. Memory grows with the data up to max and never beyond, so a build or
// a container that prints gigabytes costs at most max bytes here.
type tailBuffer struct {
	buf      []byte // len(buf) <= max; once len == max it is used as a ring
	max      int
	start    int   // index of the oldest byte when the ring is full
	total    int64 // bytes ever written
	newlines int64 // '\n' bytes ever written
	last     byte  // last byte written (0 before any write)
}

func newTailBuffer(max int) *tailBuffer {
	if max < 1 {
		max = 1
	}
	return &tailBuffer{max: max}
}

// Write implements io.Writer and never fails.
func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n == 0 {
		return 0, nil
	}
	b.total += int64(n)
	b.last = p[n-1]
	b.newlines += int64(bytes.Count(p, []byte{'\n'}))
	if len(p) >= b.max {
		if cap(b.buf) < b.max {
			b.buf = make([]byte, 0, b.max)
		}
		b.buf = append(b.buf[:0], p[len(p)-b.max:]...)
		b.start = 0
		return n, nil
	}
	if len(b.buf) < b.max {
		room := b.max - len(b.buf)
		take := min(len(p), room)
		b.grow(len(b.buf) + take)
		b.buf = append(b.buf, p[:take]...)
		p = p[take:]
		if len(p) == 0 {
			return n, nil
		}
	}
	for len(p) > 0 {
		c := copy(b.buf[b.start:], p)
		p = p[c:]
		b.start = (b.start + c) % b.max
	}
	return n, nil
}

// grow makes room for need bytes without ever reserving more than max.
func (b *tailBuffer) grow(need int) {
	if need <= cap(b.buf) {
		return
	}
	c := min(b.max, max(need, 2*cap(b.buf)))
	nb := make([]byte, len(b.buf), c)
	copy(nb, b.buf)
	b.buf = nb
}

// Bytes returns the kept bytes, oldest first, as a new slice.
func (b *tailBuffer) Bytes() []byte {
	out := make([]byte, 0, len(b.buf))
	out = append(out, b.buf[b.start:]...)
	return append(out, b.buf[:b.start]...)
}

// Dropped is the number of bytes written but no longer kept.
func (b *tailBuffer) Dropped() int64 { return b.total - int64(len(b.buf)) }

// EndsLine reports whether nothing was written yet or the last byte was '\n'.
func (b *tailBuffer) EndsLine() bool { return b.total == 0 || b.last == '\n' }

// Newlines is the number of line breaks ever written.
func (b *tailBuffer) Newlines() int64 { return b.newlines }

// alignTail makes data, the kept tail of a longer stream of which dropped
// bytes were cut off, start at a line boundary: the partial first line is cut
// up to and including the first '\n'. When there is no usable line break (a
// single huge line) only a split UTF-8 sequence at the start is cut. It
// returns the aligned data and the total number of bytes now omitted.
func alignTail(data []byte, dropped int64) ([]byte, int64) {
	if dropped <= 0 {
		return data, 0
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 && i < len(data)-1 {
		return data[i+1:], dropped + int64(i+1)
	}
	cut := 0
	for cut < len(data) && cut < utf8.UTFMax && !utf8.RuneStart(data[cut]) {
		cut++
	}
	return data[cut:], dropped + int64(cut)
}

// omittedPattern matches the header line omittedHeader writes.
var omittedPattern = regexp.MustCompile(`^（前面 ([0-9]+) KB 已省略）\n`)

// omittedHeader is the first line of a log whose beginning was cut off.
func omittedHeader(omitted int64) string {
	return fmt.Sprintf("（前面 %d KB 已省略）\n", (omitted+1023)/1024)
}

// tailText returns the kept tail of b as text that starts at a line boundary,
// prefixed with an omitted header when anything was cut off.
func tailText(b *tailBuffer) string {
	data, omitted := alignTail(b.Bytes(), b.Dropped())
	if omitted == 0 {
		return string(data)
	}
	return omittedHeader(omitted) + string(data)
}

// shrinkLog keeps at most maxBody bytes of the end of log (a text produced by
// tailText), aligned to a line boundary, and rewrites the omitted header to
// count both what was cut before and what is cut now.
func shrinkLog(log string, maxBody int) string {
	var omitted int64
	body := log
	if m := omittedPattern.FindStringSubmatch(log); m != nil {
		kb, _ := strconv.ParseInt(m[1], 10, 64)
		omitted = kb * 1024
		body = log[len(m[0]):]
	}
	if maxBody < 0 {
		maxBody = 0
	}
	if len(body) <= maxBody {
		return log
	}
	from := len(body) - maxBody
	if body[from-1] == '\n' {
		// Already at a line start: nothing partial to cut.
		return omittedHeader(omitted+int64(from)) + body[from:]
	}
	data, cut := alignTail([]byte(body[from:]), int64(from))
	return omittedHeader(omitted+cut) + string(data)
}

// lineCutMarker ends an excerpt line that was longer than maxExcerptLineBytes.
const lineCutMarker = "…（本行已截断）"

// truncateLine caps s at max bytes without splitting a UTF-8 sequence,
// appending lineCutMarker when it cut anything.
func truncateLine(s string, max int) string {
	if len(s) <= max {
		return s
	}
	i := max
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i] + lineCutMarker
}
