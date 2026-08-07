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
			dialed := false
			for _, srvAddr := range zc.servers {
				var c net.Conn
				var err error
				if zc.useTLS {
					c, err = tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", srvAddr, zc.tlsConfig)
				} else {
					c, err = net.DialTimeout("tcp", srvAddr, 5*time.Second)
				}
				if err != nil {
					connErr = err
					continue
				}
				// Assign zc.conn only on a confirmed successful dial. A
				// failed TLS handshake (e.g. rejected/untrusted client or
				// server cert) can make tls.DialWithDialer return a non-nil
				// net.Conn interface wrapping a nil *tls.Conn — the classic
				// typed-nil trap. "zc.conn == nil" below would not have
				// caught that, and calling SetDeadline on it segfaults the
				// whole process. err is the only reliable success signal.
				zc.conn = c
				dialed = true
				break
			}
			if !dialed {
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
			// The Zabbix trapper protocol is one transaction per TCP connection:
			// the server processes exactly one request then drops its side.
			// Reusing this connection for the next DoReq call doesn't error at
			// the transport level, but the server silently fails to resolve
			// items on it — values still parse, but nothing gets stored. Close
			// after every request so each call always dials fresh, matching
			// zabbix_sender/real agent behavior.
			zc.conn.Close()
			zc.conn = nil
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

			// Security: the declared uncompLen is attacker-controlled and unverified
			// against the actual deflate stream, so it cannot be trusted on its own —
			// a small compressed payload can still decompress to gigabytes (zip bomb).
			// Cap the actual decompressed output at MaxPayloadSize regardless.
			var uncompBuf bytes.Buffer
			n, err := io.CopyN(&uncompBuf, r, MaxPayloadSize+1)
			if err != nil && err != io.EOF {
				return nil, fmt.Errorf("failed to decompress response: %v", err)
			}
			if n > MaxPayloadSize {
				return nil, fmt.Errorf("decompressed payload exceeds safety limit")
			}
			// See the ZBXD\x01 branch above: one transaction per connection.
			zc.conn.Close()
			zc.conn = nil
			return uncompBuf.Bytes(), nil
		} else {
			zc.conn.Close()
			zc.conn = nil
			return nil, fmt.Errorf("invalid zabbix server response header")
		}
	}
	return nil, err
}
