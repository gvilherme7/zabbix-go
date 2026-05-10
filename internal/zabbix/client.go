package zabbix

import (
	"bytes"
	"compress/zlib"
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
	var payload []byte
	if zc.compress {
		var b bytes.Buffer
		w := zlib.NewWriter(&b)
		w.Write(jsonData)
		w.Close()
		compressedData := b.Bytes()

		header := []byte("ZBXD\x03")
		
		compLen := make([]byte, 4)
		binary.LittleEndian.PutUint32(compLen, uint32(len(compressedData)))
		
		uncompLen := make([]byte, 4)
		binary.LittleEndian.PutUint32(uncompLen, uint32(len(jsonData)))
		
		var buffer bytes.Buffer
		buffer.Write(header)
		buffer.Write(compLen)
		buffer.Write(uncompLen)
		buffer.Write(compressedData)
		payload = buffer.Bytes()
	} else {
		header := []byte("ZBXD\x01")
		dataLen := make([]byte, 8)
		binary.LittleEndian.PutUint64(dataLen, uint64(len(jsonData)))
		
		var buffer bytes.Buffer
		buffer.Write(header)
		buffer.Write(dataLen)
		buffer.Write(jsonData)
		payload = buffer.Bytes()
	}

	zc.mu.Lock()
	defer zc.mu.Unlock()

	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if zc.conn == nil {
			var connErr error
			for _, srvAddr := range zc.servers {
				if zc.useTLS {
					zc.conn, connErr = tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", srvAddr, zc.tlsConfig)
				} else {
					zc.conn, connErr = net.DialTimeout("tcp", srvAddr, 5*time.Second)
				}
				if connErr == nil {
					break
				}
			}
			if zc.conn == nil {
				return nil, fmt.Errorf("all servers failed to connect, last error: %v", connErr)
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
		if _, err = io.ReadFull(zc.conn, respHeader); err != nil {
			zc.conn.Close()
			zc.conn = nil
			continue
		}
		
		if bytes.Equal(respHeader[:5], []byte("ZBXD\x01")) {
			respLen := binary.LittleEndian.Uint64(respHeader[5:13])
			
			if respLen > MaxPayloadSize {
				zc.conn.Close()
				zc.conn = nil
				return nil, fmt.Errorf("payload size %d exceeds safety limit", respLen)
			}

			respBody := make([]byte, respLen)
			if _, err = io.ReadFull(zc.conn, respBody); err != nil {
				zc.conn.Close()
				zc.conn = nil
				continue
			}
			return respBody, nil
			
		} else if bytes.Equal(respHeader[:5], []byte("ZBXD\x03")) {
			compLen := binary.LittleEndian.Uint32(respHeader[5:9])
			uncompLen := binary.LittleEndian.Uint32(respHeader[9:13])
			
			if compLen > MaxPayloadSize || uncompLen > MaxPayloadSize {
				zc.conn.Close()
				zc.conn = nil
				return nil, fmt.Errorf("compressed payload size exceeds safety limit")
			}
			
			respBody := make([]byte, compLen)
			if _, err = io.ReadFull(zc.conn, respBody); err != nil {
				zc.conn.Close()
				zc.conn = nil
				continue
			}
			
			b := bytes.NewReader(respBody)
			r, err := zlib.NewReader(b)
			if err != nil {
				return nil, err
			}
			defer r.Close()
			
			var uncompBuf bytes.Buffer
			io.Copy(&uncompBuf, r)
			return uncompBuf.Bytes(), nil
		} else {
			zc.conn.Close()
			zc.conn = nil
			return nil, fmt.Errorf("invalid zabbix server response header")
		}
	}
	return nil, err
}
