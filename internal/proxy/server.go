package proxy

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/gvilherme7/zabbix-go/internal/buffer"
	"github.com/gvilherme7/zabbix-go/internal/zabbix"
)

var (
	proxyBufferPath = "proxy_metrics.dat"
)

type Server struct {
	Clients    []*zabbix.Client
	ListenAddr string
	UseTLS     bool
	TLSConfig  *tls.Config
	ExitChan   <-chan struct{}
}

// Start proxy starts the nano-proxy mode.
func (s *Server) Start() {
	log.Printf("Starting Lightweight Zabbix Proxy on %s (TLS: %v)", s.ListenAddr, s.UseTLS)

	var listener net.Listener
	var err error

	if s.UseTLS {
		listener, err = tls.Listen("tcp", s.ListenAddr, s.TLSConfig)
	} else {
		listener, err = net.Listen("tcp", s.ListenAddr)
	}

	if err != nil {
		log.Fatalf("Failed to start proxy listener: %v", err)
	}
	defer listener.Close()

	// Start the background forwarder
	go s.forwarder()

	// Accept loop
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-s.ExitChan:
					return
				default:
					log.Printf("Proxy accept error: %v", err)
					continue
				}
			}
			go s.handleConnection(conn)
		}
	}()

	<-s.ExitChan
	log.Println("Proxy shutting down...")
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	// Security: Strict absolute read deadline to prevent Slowloris attacks
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Read header
	header := make([]byte, 13)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}

	if !bytes.Equal(header[:5], []byte("ZBXD\x01")) {
		return // Invalid protocol
	}

	dataLen := binary.LittleEndian.Uint64(header[5:13])
	if dataLen > zabbix.MaxPayloadSize { // Sanity check: max 10MB payload to prevent OOM
		return
	}

	payload := make([]byte, dataLen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return
	}

	// Determine request type
	var genericReq struct {
		Request string `json:"request"`
	}
	if err := json.Unmarshal(payload, &genericReq); err != nil {
		return
	}

	if genericReq.Request == "sender data" {
		s.handleSenderData(conn, payload)
	} else if genericReq.Request == "active checks" {
		s.handleActiveChecks(conn, payload)
	} else {
		s.writeResponse(conn, `{"response":"failed","info":"unknown request"}`)
	}
}

func (s *Server) handleSenderData(conn net.Conn, payload []byte) {
	var packet zabbix.ZabbixPacket
	if err := json.Unmarshal(payload, &packet); err != nil {
		s.writeResponse(conn, `{"response":"failed","info":"invalid JSON"}`)
		return
	}

	// Buffer to encrypted/plaintext disk
	if err := buffer.WriteMetrics(proxyBufferPath, packet.Data); err != nil {
		s.writeResponse(conn, `{"response":"failed","info":"proxy disk error"}`)
		return
	}

	// Send success
	resp := fmt.Sprintf(`{"response":"success","info":"processed: %d; failed: 0; total: %d; seconds spent: 0.00000"}`, len(packet.Data), len(packet.Data))
	s.writeResponse(conn, resp)
}

func (s *Server) handleActiveChecks(conn net.Conn, payload []byte) {
	// Query upstream servers until one successfully responds
	var respBytes []byte
	var err error
	for _, client := range s.Clients {
		respBytes, err = client.DoReq(payload)
		if err == nil {
			break
		}
	}

	if err != nil {
		s.writeResponse(conn, `{"response":"failed","info":"central server unreachable"}`)
		return
	}

	// Relay the response back to agent
	header := []byte("ZBXD\x01")
	dataLen := make([]byte, 8)
	binary.LittleEndian.PutUint64(dataLen, uint64(len(respBytes)))

	conn.Write(header)
	conn.Write(dataLen)
	conn.Write(respBytes)
}

func (s *Server) writeResponse(conn net.Conn, jsonStr string) {
	respBytes := []byte(jsonStr)
	header := []byte("ZBXD\x01")
	dataLen := make([]byte, 8)
	binary.LittleEndian.PutUint64(dataLen, uint64(len(respBytes)))

	conn.Write(header)
	conn.Write(dataLen)
	conn.Write(respBytes)
}

func (s *Server) forwarder() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.flushBuffer()
		case <-s.ExitChan:
			s.flushBuffer()
			return
		}
	}
}

func (s *Server) flushBuffer() {
	metrics := buffer.Flush(proxyBufferPath)
	if len(metrics) > 0 {
		// Batch send in chunks to avoid massive single payloads
		chunkSize := 1000
		for i := 0; i < len(metrics); i += chunkSize {
			end := i + chunkSize
			if end > len(metrics) {
				end = len(metrics)
			}
			chunk := metrics[i:end]
			s.sendDataRaw(chunk)
		}
	}
}

func (s *Server) sendDataRaw(metrics []zabbix.Metric) {
	packet := zabbix.ZabbixPacket{
		Request: "sender data",
		Data:    metrics,
	}
	jsonData, _ := json.Marshal(packet)
	
	success := false
	for _, client := range s.Clients {
		if _, err := client.DoReq(jsonData); err == nil {
			success = true
		}
	}

	if !success {
		// Re-buffer if failed to send to ALL servers
		buffer.WriteMetrics(proxyBufferPath, metrics)
	}
}
