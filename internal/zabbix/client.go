package zabbix

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	MaxPayloadSize = 10 * 1024 * 1024 // 10 MB limit to prevent OOM from rogue servers
)

func (zc *Client) DoReq(jsonData []byte) ([]byte, error) {
	header := []byte("ZBXD\x01")
	dataLen := make([]byte, 8)
	binary.LittleEndian.PutUint64(dataLen, uint64(len(jsonData)))
	
	var buffer bytes.Buffer
	buffer.Write(header)
	buffer.Write(dataLen)
	buffer.Write(jsonData)
	payload := buffer.Bytes()

	zc.mu.Lock()
	defer zc.mu.Unlock()

	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if zc.conn == nil {
			if zc.useTLS {
				zc.conn, err = tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", zc.serverAddr, zc.tlsConfig)
			} else {
				zc.conn, err = net.DialTimeout("tcp", zc.serverAddr, 5*time.Second)
			}
			if err != nil {
				zc.conn = nil
				return nil, err
			}
		}

		// Security: Strict absolute deadline for the entire transaction (Write + Read)
		// Prevents Slowloris / socket lockups.
		zc.conn.SetDeadline(time.Now().Add(10 * time.Second))
		
		if _, err = zc.conn.Write(payload); err != nil {
			zc.conn.Close()
			zc.conn = nil
			continue
		}

		respHeader := make([]byte, 13)
		if _, err := io.ReadFull(zc.conn, respHeader); err != nil {
			zc.conn.Close()
			zc.conn = nil
			continue
		}
		
		if !bytes.Equal(respHeader[:5], header) {
			zc.conn.Close()
			zc.conn = nil
			return nil, fmt.Errorf("invalid zabbix server response header")
		}
		
		respLen := binary.LittleEndian.Uint64(respHeader[5:13])
		
		// Security: OOM Protection Check
		if respLen > MaxPayloadSize {
			zc.conn.Close()
			zc.conn = nil
			return nil, fmt.Errorf("payload size %d exceeds safety limit of %d bytes, dropping to prevent OOM", respLen, MaxPayloadSize)
		}

		respBody := make([]byte, respLen)
		if _, err := io.ReadFull(zc.conn, respBody); err != nil {
			zc.conn.Close()
			zc.conn = nil
			continue
		}

		return respBody, nil
	}
	return nil, err
}
