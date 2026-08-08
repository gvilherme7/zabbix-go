package test

import (
	"os"
	"testing"

	"github.com/gvilherme7/zabbix-go/internal/buffer"
	"github.com/gvilherme7/zabbix-go/internal/zabbix"
)

func TestBufferSizeCap(t *testing.T) {
	path := "cap_test.dat"
	os.Remove(path)
	defer os.Remove(path)

	origCap := buffer.MaxBufferBytes
	buffer.MaxBufferBytes = 500
	defer func() { buffer.MaxBufferBytes = origCap }()

	metrics := []zabbix.Metric{{Host: "h", Key: "k", Value: "some reasonably sized value to fill bytes faster 0123456789"}}
	for i := 0; i < 50; i++ {
		if err := buffer.WriteMetrics(path, metrics); err != nil {
			t.Fatalf("WriteMetrics failed: %v", err)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("buffer file missing: %v", err)
	}
	// A little slack: the size check happens before each batch write, so the
	// file can exceed the cap by roughly one batch's worth of bytes, never by
	// anywhere close to unbounded growth (50 batches would be ~5.5KB unguarded).
	if info.Size() > buffer.MaxBufferBytes+300 {
		t.Fatalf("buffer grew to %d bytes despite a %d byte cap — unbounded growth not prevented", info.Size(), buffer.MaxBufferBytes)
	}
	t.Logf("Buffer capped at %d bytes after 50 write attempts (cap: %d)", info.Size(), buffer.MaxBufferBytes)
}
