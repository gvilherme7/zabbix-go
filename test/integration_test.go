package test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gvilherme7/zabbix-go/internal/proxy"
	"github.com/gvilherme7/zabbix-go/internal/zabbix"
)

func TestProxyEnterpriseStress(t *testing.T) {
	// 1. Slow Central Server to force disk buffering
	serverLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer serverLn.Close()

	go func() {
		for {
			conn, err := serverLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					if _, err := c.Read(buf); err != nil {
						break
					}
					time.Sleep(1 * time.Millisecond)
				}
			}(conn)
		}
	}()

	client := zabbix.NewClient(serverLn.Addr().String(), false, nil)
	exitChan := make(chan struct{})
	defer close(exitChan)

	srv := &proxy.Server{
		Client:     client,
		ListenAddr: "127.0.0.1:10056",
		UseTLS:     false,
		ExitChan:   exitChan,
	}
	go srv.Start()
	time.Sleep(500 * time.Millisecond)

	var wg sync.WaitGroup
	agentCount := 100
	metricsPerAgent := 500

	start := time.Now()

	for i := 0; i < agentCount; i++ {
		wg.Add(1)
		go func(agentID int) {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", "127.0.0.1:10056", 10*time.Second)
			if err != nil {
				t.Errorf("Agent %d connection failed: %v", agentID, err)
				return
			}
			defer conn.Close()

			var metrics []zabbix.Metric
			for j := 0; j < metricsPerAgent; j++ {
				metrics = append(metrics, zabbix.Metric{Host: "ent-host", Key: "cpu.load", Value: "0.5"})
			}
			packet := zabbix.ZabbixPacket{Request: "sender data", Data: metrics}
			jsonData, _ := json.Marshal(packet)

			header := []byte("ZBXD\x01")
			dataLen := make([]byte, 8)
			binary.LittleEndian.PutUint64(dataLen, uint64(len(jsonData)))

			conn.Write(header)
			conn.Write(dataLen)
			conn.Write(jsonData)

			respHeader := make([]byte, 13)
			io.ReadFull(conn, respHeader)
			respLen := binary.LittleEndian.Uint64(respHeader[5:13])
			respBody := make([]byte, respLen)
			io.ReadFull(conn, respBody)
		}(i)
	}

	wg.Wait()
	ingestDuration := time.Since(start)
	t.Logf("Enterprise Stress Complete: %d metrics ingested in %v", agentCount*metricsPerAgent, ingestDuration)
}

func TestOOMProtection(t *testing.T) {
	// Start a rogue server that sends a massive length header
	serverLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer serverLn.Close()

	go func() {
		conn, err := serverLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		buf := make([]byte, 1024)
		conn.Read(buf)

		// Send rogue response with 2GB length header
		header := []byte("ZBXD\x01")
		dataLen := make([]byte, 8)
		binary.LittleEndian.PutUint64(dataLen, uint64(2*1024*1024*1024)) // 2GB

		conn.Write(header)
		conn.Write(dataLen)
		// Do not send actual payload, just the header
	}()

	client := zabbix.NewClient(serverLn.Addr().String(), false, nil)
	_, err = client.DoReq([]byte(`{"request":"test"}`))
	
	if err == nil {
		t.Fatalf("Expected error from OOM protection, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("exceeds safety limit")) {
		t.Fatalf("Expected OOM protection error message, got: %v", err)
	}
	t.Logf("OOM Protection Successfully blocked massive payload: %v", err)
}
