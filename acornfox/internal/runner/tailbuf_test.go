package runner

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTailBufferKeepsLastBytes(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, max := range []int{1, 2, 7, 16, 100} {
		for round := 0; round < 200; round++ {
			b := newTailBuffer(max)
			var all []byte
			for w := rng.Intn(12); w > 0; w-- {
				p := make([]byte, rng.Intn(3*max+1))
				rng.Read(p)
				all = append(all, p...)
				if n, err := b.Write(p); n != len(p) || err != nil {
					t.Fatalf("Write = %d, %v", n, err)
				}
			}
			want := all
			if len(want) > max {
				want = want[len(want)-max:]
			}
			if got := b.Bytes(); !bytes.Equal(got, want) {
				t.Fatalf("max=%d: kept %q, want %q", max, got, want)
			}
			if b.Dropped() != int64(len(all)-len(want)) {
				t.Fatalf("max=%d: dropped %d, want %d", max, b.Dropped(), len(all)-len(want))
			}
			if b.Newlines() != int64(bytes.Count(all, []byte{'\n'})) {
				t.Fatalf("newline count %d", b.Newlines())
			}
			if len(b.buf) > max || cap(b.buf) > max {
				t.Fatalf("buffer grew past bound: len %d cap %d max %d", len(b.buf), cap(b.buf), max)
			}
		}
	}
}

func TestTailBufferBoundaries(t *testing.T) {
	b := newTailBuffer(4)
	b.Write([]byte("abcd")) // exactly full
	if got := string(b.Bytes()); got != "abcd" || b.Dropped() != 0 {
		t.Fatalf("full: %q dropped %d", got, b.Dropped())
	}
	b.Write([]byte("e"))
	if got := string(b.Bytes()); got != "bcde" || b.Dropped() != 1 {
		t.Fatalf("wrap: %q dropped %d", got, b.Dropped())
	}
	b.Write([]byte("0123456789"))
	if got := string(b.Bytes()); got != "6789" {
		t.Fatalf("oversized write: %q", got)
	}
	b.Write(nil)
	if b.EndsLine() || string(b.Bytes()) != "6789" {
		t.Fatalf("empty write changed state")
	}
	b.Write([]byte("\n"))
	if !b.EndsLine() {
		t.Fatalf("EndsLine after newline")
	}
}

func TestAlignTail(t *testing.T) {
	cases := []struct {
		name      string
		data      string
		dropped   int64
		want      string
		wantOmit  int64
		wantValid bool
	}{
		{"nothing dropped", "partial\nline\n", 0, "partial\nline\n", 0, true},
		{"cut partial first line", "tial\nnext\nlast\n", 10, "next\nlast\n", 15, true},
		{"only trailing newline", "huge single line\n", 100, "huge single line\n", 100, true},
		{"no newline", "abc", 5, "abc", 5, true},
		{"split utf-8 without newline", "\x96\x87字", 3, "字", 5, true},
		{"split utf-8 before newline", "\x87字\n下一行\n", 2, "下一行\n", 2 + int64(len("\x87字\n")), true},
	}
	for _, c := range cases {
		got, omit := alignTail([]byte(c.data), c.dropped)
		if string(got) != c.want || omit != c.wantOmit {
			t.Errorf("%s: got %q omitted %d, want %q omitted %d", c.name, got, omit, c.want, c.wantOmit)
		}
		if !utf8.Valid(got) {
			t.Errorf("%s: invalid UTF-8 %q", c.name, got)
		}
	}
}

func TestTailTextHeaderAndUTF8(t *testing.T) {
	b := newTailBuffer(64)
	for i := 0; i < 200; i++ {
		b.Write([]byte("第" + strings.Repeat("字", i%7) + "行\n"))
	}
	text := tailText(b)
	if !strings.HasPrefix(text, "（前面 ") || !strings.Contains(text, " KB 已省略）\n第") {
		t.Fatalf("missing header or not aligned to a line start: %q", text)
	}
	if !utf8.ValidString(text) {
		t.Fatalf("invalid UTF-8: %q", text)
	}
	small := newTailBuffer(64)
	small.Write([]byte("short\n"))
	if got := tailText(small); got != "short\n" {
		t.Fatalf("untruncated text changed: %q", got)
	}
}

func TestOmittedHeaderRoundsUp(t *testing.T) {
	for omitted, want := range map[int64]string{1: "1", 1024: "1", 1025: "2", 512 << 10: "512"} {
		if got := omittedHeader(omitted); got != "（前面 "+want+" KB 已省略）\n" {
			t.Errorf("omittedHeader(%d) = %q", omitted, got)
		}
	}
}

func TestShrinkLogAccumulatesHeader(t *testing.T) {
	body := strings.Repeat("0123456789abcde\n", 1024) // 16 KiB
	log := omittedHeader(4096) + body
	got := shrinkLog(log, 8<<10)
	m := omittedPattern.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no header: %q", got[:40])
	}
	// 4 KiB before + 8 KiB cut now = 12 KiB.
	if m[1] != "12" {
		t.Fatalf("omitted = %s KB, want 12", m[1])
	}
	rest := got[len(m[0]):]
	if len(rest) > 8<<10 || !strings.HasPrefix(rest, "0123456789abcde\n") {
		t.Fatalf("body not bounded or aligned: len %d start %q", len(rest), rest[:20])
	}
	if same := shrinkLog("short\n", 100); same != "short\n" {
		t.Fatalf("short log changed: %q", same)
	}
	if empty := shrinkLog(body, -5); !strings.HasPrefix(empty, "（前面 16 KB 已省略）") {
		t.Fatalf("negative bound: %q", empty)
	}
}

func TestTruncateLineUTF8(t *testing.T) {
	if got := truncateLine("abc", 3); got != "abc" {
		t.Fatalf("no cut: %q", got)
	}
	got := truncateLine("ab字字", 4) // "字" is 3 bytes; cut must not split it
	if got != "ab"+lineCutMarker {
		t.Fatalf("cut: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8")
	}
}
