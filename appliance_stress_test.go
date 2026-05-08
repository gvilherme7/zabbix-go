package main

import (
	"fmt"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestLowMemoryApplianceStress(t *testing.T) {
	// Appliance constraints: Simulate an embedded device
	const memoryBudgetBytes = 5 * 1024 * 1024 // 5 MB hard limit
	var peakMemory uint64

	addr := "127.0.0.1:10054"
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to start appliance mock server: %v", err)
	}
	defer l.Close()

	var bytesReceived uint64

	// Mock server reading payloads
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 32768)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						atomic.AddUint64(&bytesReceived, uint64(n))
					}
					if err != nil {
						break
					}
					// Send Zabbix standard success reply
					respBody := []byte(`{"response":"success"}`)
					resp := make([]byte, 13+len(respBody))
					copy(resp[:5], []byte("ZBXD\x01"))
					resp[5] = byte(len(respBody))
					copy(resp[13:], respBody)
					c.Write(resp)
					break
				}
			}(conn)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	// Background Memory Watchdog: polls heap every 2 milliseconds
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				current := m.Alloc
				for {
					peak := atomic.LoadUint64(&peakMemory)
					if current > peak {
						if atomic.CompareAndSwapUint64(&peakMemory, peak, current) {
							break
						}
					} else {
						break
					}
				}
			}
		}
	}()

	// Construct huge payload for the appliance to handle
	payloadSize := 200 // 200 metrics per request
	metrics := make([]Metric, payloadSize)
	for i := 0; i < payloadSize; i++ {
		metrics[i] = Metric{
			Host:  "EmbeddedDevice01",
			Key:   fmt.Sprintf("sensor.thermal.zone[%d]", i),
			Value: "42.5",
		}
	}

	runtime.GC()
	fmt.Println("\n=============================================")
	fmt.Println("--- Embedded Appliance Low-Memory Test ---")
	fmt.Printf("Memory Budget: %.2f MB\n", float64(memoryBudgetBytes)/(1024*1024))
	fmt.Println("=============================================")

	globalClient = &ZabbixClient{
		serverAddr: addr,
	}

	start := time.Now()
	numRequests := 5000 // Send 5000 continuous requests (1 Million Metrics)

	for i := 0; i < numRequests; i++ {
		err := sendData(addr, "EmbeddedDevice01", metrics)
		if err != nil {
			t.Fatalf("Send failed: %v", err)
		}
	}

	duration := time.Since(start)
	close(done)

	finalPeak := atomic.LoadUint64(&peakMemory)
	totalBytes := atomic.LoadUint64(&bytesReceived)

	fmt.Printf("Total Requests Sent:    %d\n", numRequests)
	fmt.Printf("Total Metrics Sent:     %d\n", numRequests*payloadSize)
	fmt.Printf("Total Time Elapsed:     %v\n", duration)
	fmt.Printf("Throughput (Req/sec):   %.2f reqs/s\n", float64(numRequests)/duration.Seconds())
	fmt.Printf("Peak Memory Reached:    %.2f MB\n", float64(finalPeak)/(1024*1024))
	fmt.Printf("Total Bandwidth Pushed: %.2f MB\n", float64(totalBytes)/(1024*1024))

	if finalPeak > memoryBudgetBytes {
		t.Fatalf("\nFAILED: Agent exceeded memory budget! Peak: %.2f MB (Budget: %.2f MB)", float64(finalPeak)/(1024*1024), float64(memoryBudgetBytes)/(1024*1024))
	} else {
		fmt.Printf("\nPASSED: Agent remained well within appliance constraints.\n")
	}
	fmt.Println("=============================================")
}
