package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// mockZabbixServer spins up a local TCP server that understands just enough
// of the Zabbix Protocol to accept data and acknowledge it.
func mockZabbixServer(t *testing.T, addr string, expectedMetrics int) net.Listener {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}

	go func() {
		// Accept a single connection for the test
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// 1. Read the Zabbix Header (13 bytes total: 5 byte identifier + 8 byte length)
		header := make([]byte, 13)
		if _, err := io.ReadFull(conn, header); err != nil {
			t.Errorf("Failed to read header: %v", err)
			return
		}

		if !bytes.Equal(header[:5], []byte("ZBXD\x01")) {
			t.Errorf("Invalid Zabbix protocol header received")
			return
		}

		dataLen := binary.LittleEndian.Uint64(header[5:13])

		// 2. Read the JSON Payload
		data := make([]byte, dataLen)
		if _, err := io.ReadFull(conn, data); err != nil {
			t.Errorf("Failed to read data payload: %v", err)
			return
		}

		var packet ZabbixPacket
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Errorf("Failed to parse JSON payload: %v", err)
			return
		}

		// 3. Verify the payload
		if packet.Request != "sender data" {
			t.Errorf("Expected request 'sender data', got '%s'", packet.Request)
		}
		if len(packet.Data) != expectedMetrics {
			t.Errorf("Expected %d metrics, got %d", expectedMetrics, len(packet.Data))
		}

		// 4. Send Zabbix acknowledgment back to the agent
		respBody := []byte(`{"response":"success","info":"processed: 3; failed: 0; total: 3; seconds spent: 0.000000"}`)
		
		respHeader := make([]byte, 13)
		copy(respHeader[:5], []byte("ZBXD\x01"))
		binary.LittleEndian.PutUint64(respHeader[5:13], uint64(len(respBody)))

		conn.Write(respHeader)
		conn.Write(respBody)
	}()

	return l
}

func TestSendData(t *testing.T) {
	addr := "127.0.0.1:10052" // Use a different port in case 10051 is in use
	
	// Create some dummy metrics to send
	metrics := []Metric{
		{Host: "TestHost", Key: "test.key1", Value: "100"},
		{Host: "TestHost", Key: "test.key2", Value: "200"},
	}

	// Spin up the fake Zabbix server
	listener := mockZabbixServer(t, addr, len(metrics))
	defer listener.Close()

	// Give the server a fraction of a second to start listening
	time.Sleep(100 * time.Millisecond)

	globalClient = &ZabbixClient{
		serverAddr: addr,
	}

	// Call our actual sendData function from main.go
	err := sendData(addr, "TestHost", metrics)
	if err != nil {
		t.Fatalf("sendData failed: %v", err)
	}
}

func TestUserParameters(t *testing.T) {
	configContent := []byte("UserParameter=test.ping,echo pong\nUserParameter=test.err,exit 1\n")
	os.WriteFile("test_zabbix.conf", configContent, 0644)
	defer os.Remove("test_zabbix.conf")

	_, err := ParseConfig("test_zabbix.conf")
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	plugin, ok := Registry["test.ping"]
	if !ok {
		t.Fatalf("test.ping not registered")
	}

	val, err := plugin.Collect()
	if err != nil {
		t.Fatalf("test.ping failed: %v", err)
	}
	if val != "pong" {
		t.Fatalf("Expected 'pong', got '%s'", val)
	}
}

func TestParseDelay(t *testing.T) {
	if d := parseDelay("30"); d != 30 {
		t.Errorf("Expected 30, got %d", d)
	}
	if d := parseDelay("1m"); d != 60 {
		t.Errorf("Expected 60, got %d", d)
	}
	if d := parseDelay("invalid"); d != 60 {
		t.Errorf("Expected 60, got %d", d)
	}
}
