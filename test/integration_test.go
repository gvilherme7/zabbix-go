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

	clientToProxy := zabbix.NewClient(serverLn.Addr().String(), false, nil)
	proxyExit := make(chan struct{})
	proxySrv := &proxy.Server{
		Clients:    []*zabbix.Client{clientToProxy},
		ListenAddr: "127.0.0.1:10056",
		UseTLS:     false,
		ExitChan:   proxyExit,
	}
	go proxySrv.Start()
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

func TestFailoverConnectivity(t *testing.T) {
	// Setup Server1 (fails to connect), Server2 (succeeds)
	server2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start server2: %v", err)
	}
	defer server2.Close()

	go func() {
		conn, err := server2.Accept()
		if err == nil {
			defer conn.Close()
			buf := make([]byte, 1024)
			conn.Read(buf) // read header
			
			// Send valid response
			resp := []byte(`{"response":"success"}`)
			header := []byte("ZBXD\x01")
			dataLen := make([]byte, 8)
			binary.LittleEndian.PutUint64(dataLen, uint64(len(resp)))
			conn.Write(header)
			conn.Write(dataLen)
			conn.Write(resp)
		}
	}()

	// Server1 is a dead port
	deadAddr := "127.0.0.1:65534"
	servers := deadAddr + "," + server2.Addr().String()
	
	client := zabbix.NewClient(servers, false, nil)
	
	start := time.Now()
	resp, err := client.DoReq([]byte(`{"request":"test"}`))
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Failover request failed: %v", err)
	}

	if !bytes.Contains(resp, []byte("success")) {
		t.Fatalf("Expected success response, got: %s", string(resp))
	}
	
	t.Logf("Failover successful in %v", duration)
}

func TestSlowlorisProtection(t *testing.T) {
	// Setup a malicious server that drips data very slowly
	server, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start slow server: %v", err)
	}
	defer server.Close()

	go func() {
		conn, err := server.Accept()
		if err == nil {
			defer conn.Close()
			buf := make([]byte, 1024)
			conn.Read(buf) // read request
			
			header := []byte("ZBXD\x01")
			dataLen := make([]byte, 8)
			binary.LittleEndian.PutUint64(dataLen, uint64(1024))
			conn.Write(header)
			conn.Write(dataLen)
			
			// Drip feed 1 byte per second
			for i := 0; i < 1024; i++ {
				_, err := conn.Write([]byte{0x00})
				if err != nil {
					break // connection closed by client timeout
				}
				time.Sleep(1 * time.Second)
			}
		}
	}()

	client := zabbix.NewClient(server.Addr().String(), false, nil)
	
	start := time.Now()
	_, err = client.DoReq([]byte(`{"request":"test"}`))
	duration := time.Since(start)

	if err == nil {
		t.Fatalf("Expected timeout error due to Slowloris protection, got nil")
	}
	
	// The timeout is set to 10 seconds in client.go, allowing up to 15s to catch edge cases
	if duration > 15*time.Second {
		t.Fatalf("Slowloris protection failed to cut off connection within reasonable time. Duration: %v", duration)
	}

	t.Logf("Slowloris protection successfully terminated connection after %v with error: %v", duration, err)
}
