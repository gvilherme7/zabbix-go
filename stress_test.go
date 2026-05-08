package main

import (
	"fmt"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestStressAgent(t *testing.T) {
	addr := "127.0.0.1:10053"

	// Trackers
	var bytesReceived uint64
	var requestsReceived uint64

	// Mock Zabbix Server specialized to just consume data fast
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer l.Close()

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

					// We know the agent disconnects immediately after we reply with the 13-byte Zabbix header plus some JSON
					respBody := []byte(`{"response":"success"}`)
					resp := make([]byte, 13+len(respBody))
					copy(resp[:5], []byte("ZBXD\x01"))
					resp[5] = byte(len(respBody))
					copy(resp[13:], respBody)
					
					c.Write(resp)
					atomic.AddUint64(&requestsReceived, 1)
					break 
				}
			}(conn)
		}
	}()

	time.Sleep(100 * time.Millisecond) // Give server time to start

	fmt.Println("\n=============================================")
	fmt.Println("--- Starting Stress & Performance Test ---")
	fmt.Println("=============================================")

	payloadSize := 100 // We will send 100 simulated CPU/Memory metrics per request
	metrics := make([]Metric, payloadSize)
	for i := 0; i < payloadSize; i++ {
		metrics[i] = Metric{
			Host:  "StressHost",
			Key:   fmt.Sprintf("system.cpu.core[%d].load", i),
			Value: "42.0",
		}
	}

	// Give the server a fraction of a second to start listening
	time.Sleep(100 * time.Millisecond)

	globalClient = &ZabbixClient{
		serverAddr: addr,
	}

	// Trigger a garbage collection before test to get clean memory stats
	runtime.GC()
	
	start := time.Now()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Send 2000 continuous massive payloads 
	// (Simulates months of runtime, or massive metrics pushed all at once)
	numRequests := 2000
	for i := 0; i < numRequests; i++ {
		err := sendData(addr, "StressHost", metrics)
		if err != nil {
			t.Fatalf("Send failed on iteration %d: %v", i, err)
		}
	}

	duration := time.Since(start)

	// Read memory after
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	totalBytes := atomic.LoadUint64(&bytesReceived)

	fmt.Printf("Total Requests Sent:   %d\n", numRequests)
	fmt.Printf("Total Metrics Sent:    %d\n", numRequests*payloadSize)
	fmt.Printf("Total Time Elapsed:    %v\n", duration)
	fmt.Printf("Throughput (Req/sec):  %.2f reqs/s\n", float64(numRequests)/duration.Seconds())
	fmt.Printf("Throughput (Met/sec):  %.2f metrics/s\n", float64(numRequests*payloadSize)/duration.Seconds())

	fmt.Printf("\n--- Bandwidth Usage ---\n")
	fmt.Printf("Total Bandwidth (Sent payloads): %.2f MB\n", float64(totalBytes)/(1024*1024))
	fmt.Printf("Average Payload per Request:     %.2f KB\n", float64(totalBytes)/float64(numRequests)/1024)

	fmt.Printf("\n--- Memory Usage ---\n")
	// m.TotalAlloc is cumulative allocations. We look at the delta.
	allocDelta := m2.TotalAlloc - m1.TotalAlloc
	fmt.Printf("Total Data Allocated (Cumulative): %.2f MB\n", float64(allocDelta)/(1024*1024))
	fmt.Printf("Memory Allocated per Request:      %d Bytes\n", allocDelta/uint64(numRequests))
	
	// m.Alloc is current live Heap size. 
	fmt.Printf("Active Heap Size (Memory Footprint): %.2f MB\n", float64(m2.Alloc)/(1024*1024))
	fmt.Println("=============================================")
}
