package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Generate in-memory certificates for testing
func generateTestCerts() (*tls.Config, *tls.Config, error) {
	// 1. Generate CA
	caPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	caTemplate := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Zabbix Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caBytes, _ := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caPriv.PublicKey, caPriv)
	caCert, _ := x509.ParseCertificate(caBytes)
	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	// 2. Generate Server Cert
	serverPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	serverBytes, _ := x509.CreateCertificate(rand.Reader, &serverTemplate, caCert, &serverPriv.PublicKey, caPriv)
	serverTLSCert := tls.Certificate{
		Certificate: [][]byte{serverBytes},
		PrivateKey:  serverPriv,
	}

	// 3. Generate Client Cert
	clientPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	clientTemplate := x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "Zabbix Agent"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	clientBytes, _ := x509.CreateCertificate(rand.Reader, &clientTemplate, caCert, &clientPriv.PublicKey, caPriv)
	clientTLSCert := tls.Certificate{
		Certificate: [][]byte{clientBytes},
		PrivateKey:  clientPriv,
	}

	serverConfig := &tls.Config{
		Certificates: []tls.Certificate{serverTLSCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}

	clientConfig := &tls.Config{
		Certificates:       []tls.Certificate{clientTLSCert},
		RootCAs:            caPool,
		ServerName:         "127.0.0.1",
		ClientSessionCache: tls.NewLRUClientSessionCache(32),
	}

	return serverConfig, clientConfig, nil
}

// Generate an attacker CA that the agent doesn't trust
func generateAttackerCerts() *tls.Config {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	template := x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	certBytes, _ := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	tlsCert := tls.Certificate{
		Certificate: [][]byte{certBytes},
		PrivateKey:  priv,
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}
}

// Emulate a Sniffer/MITM Proxy
func startSnifferProxy(listenAddr, targetAddr string, captureBuffer *bytes.Buffer) (net.Listener, error) {
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			clientConn, err := l.Accept()
			if err != nil {
				return
			}
			serverConn, err := net.Dial("tcp", targetAddr)
			if err != nil {
				clientConn.Close()
				continue
			}
			// Sniff Client -> Server
			go func() {
				buf := make([]byte, 8192)
				for {
					n, err := clientConn.Read(buf)
					if n > 0 {
						captureBuffer.Write(buf[:n])
						serverConn.Write(buf[:n])
					}
					if err != nil {
						serverConn.Close()
						break
					}
				}
			}()
			// Pass Server -> Client
			go func() {
				buf := make([]byte, 8192)
				for {
					n, err := serverConn.Read(buf)
					if n > 0 {
						clientConn.Write(buf[:n])
					}
					if err != nil {
						clientConn.Close()
						break
					}
				}
			}()
		}
	}()
	return l, nil
}

func startMockServer(addr string, tlsConfig *tls.Config) net.Listener {
	var l net.Listener
	var err error
	if tlsConfig != nil {
		l, err = tls.Listen("tcp", addr, tlsConfig)
	} else {
		l, err = net.Listen("tcp", addr)
	}
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 8192)
				for {
					n, err := c.Read(buf)
					if err != nil || n == 0 {
						break
					}
					// Only respond if we received the JSON payload
					if strings.Contains(string(buf[:n]), "request") {
						respBody := []byte(`{"response":"success"}`)
						resp := make([]byte, 13+len(respBody))
						copy(resp[:5], []byte("ZBXD\x01"))
						resp[5] = byte(len(respBody))
						copy(resp[13:], respBody)
						c.Write(resp)
					}
				}
			}(conn)
		}
	}()
	return l
}

func TestSecurityAndChaos(t *testing.T) {
	fmt.Println("\n=======================================================")
	fmt.Println("--- SECURITY, CHAOS & APPLIANCE EMULATION REPORT ---")
	fmt.Println("=======================================================")

	metrics := []Metric{{Host: "SecureAppliance", Key: "secret.data", Value: "CONFIDENTIAL_12345"}}

	// 1. Unencrypted Sniffing Test
	t.Run("Unencrypted Connection Sniffing", func(t *testing.T) {
		serverAddr := "127.0.0.1:20051"
		proxyAddr := "127.0.0.1:20052"
		var capturedData bytes.Buffer

		srv := startMockServer(serverAddr, nil)
		defer srv.Close()
		proxy, _ := startSnifferProxy(proxyAddr, serverAddr, &capturedData)
		defer proxy.Close()

		globalClient = &ZabbixClient{serverAddr: proxyAddr, useTLS: false}
		
		err := sendData(proxyAddr, "SecureAppliance", metrics)
		if err != nil {
			t.Fatalf("Failed to send unencrypted: %v", err)
		}
		
		time.Sleep(100 * time.Millisecond) // Wait for proxy to flush

		capturedStr := capturedData.String()
		if !strings.Contains(capturedStr, "CONFIDENTIAL_12345") {
			t.Fatalf("Expected sniffer to capture plaintext 'CONFIDENTIAL_12345', but it didn't")
		}
		fmt.Println("[VULNERABILITY PROVED] Unencrypted connections leak full JSON telemetry to packet sniffers.")
	})

	// 2. Encrypted mTLS Sniffing Test
	serverConf, clientConf, _ := generateTestCerts()
	
	t.Run("Encrypted mTLS Connection Sniffing", func(t *testing.T) {
		serverAddr := "127.0.0.1:20053"
		proxyAddr := "127.0.0.1:20054"
		var capturedData bytes.Buffer

		srv := startMockServer(serverAddr, serverConf)
		defer srv.Close()
		proxy, _ := startSnifferProxy(proxyAddr, serverAddr, &capturedData)
		defer proxy.Close()

		globalClient = &ZabbixClient{serverAddr: proxyAddr, useTLS: true, tlsConfig: clientConf}
		
		err := sendData(proxyAddr, "SecureAppliance", metrics)
		if err != nil {
			t.Fatalf("Failed to send encrypted: %v", err)
		}
		
		time.Sleep(100 * time.Millisecond)

		capturedStr := capturedData.String()
		if strings.Contains(capturedStr, "CONFIDENTIAL_12345") || strings.Contains(capturedStr, "sender data") {
			t.Fatalf("CRITICAL FAILURE: mTLS leaked plaintext to sniffer!")
		}
		fmt.Println("[SECURITY VERIFIED] mTLS perfectly masks payload. Packet sniffer sees only cryptographic noise.")
	})

	// 3. Malicious Rogue Server (MITM Attack)
	t.Run("Malicious Attacker (Rogue Server)", func(t *testing.T) {
		rogueAddr := "127.0.0.1:20055"
		rogueConf := generateAttackerCerts()
		
		srv := startMockServer(rogueAddr, rogueConf)
		defer srv.Close()

		globalClient = &ZabbixClient{serverAddr: rogueAddr, useTLS: true, tlsConfig: clientConf}
		
		err := sendData(rogueAddr, "SecureAppliance", metrics)
		if err == nil {
			t.Fatalf("CRITICAL FAILURE: Agent connected to an untrusted rogue server!")
		}
		
		if !strings.Contains(err.Error(), "certificate signed by unknown authority") {
			t.Fatalf("Agent rejected connection, but for wrong reason: %v", err)
		}
		fmt.Println("[SECURITY VERIFIED] Agent successfully blocked MITM Rogue Server connection due to strict CA enforcement.")
	})

	// 4. Low-Memory Appliance mTLS Stress Test under OS Chaos
	t.Run("mTLS Low-Memory Appliance Stress", func(t *testing.T) {
		serverAddr := "127.0.0.1:20056"
		srv := startMockServer(serverAddr, serverConf)
		defer srv.Close()

		globalClient = &ZabbixClient{serverAddr: serverAddr, useTLS: true, tlsConfig: clientConf}
		
		// Background Memory Watchdog
		const memoryBudgetBytes = 5 * 1024 * 1024 // 5 MB
		var peakMemory uint64
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

		payloadSize := 200
		stressMetrics := make([]Metric, payloadSize)
		for i := 0; i < payloadSize; i++ {
			stressMetrics[i] = Metric{Host: "ChaosAppliance", Key: fmt.Sprintf("sensor[%d]", i), Value: "1"}
		}

		runtime.GC()
		start := time.Now()
		numRequests := 2000 // 400,000 metrics
		
		for i := 0; i < numRequests; i++ {
			err := sendData(serverAddr, "ChaosAppliance", stressMetrics)
			if err != nil {
				t.Fatalf("Send failed under mTLS stress: %v", err)
			}
		}

		duration := time.Since(start)
		close(done)

		finalPeak := atomic.LoadUint64(&peakMemory)
		
		fmt.Printf("[PERFORMANCE VERIFIED] Encrypted mTLS Stress Test completed.\n")
		fmt.Printf("   -> Total Requests:  %d\n", numRequests)
		fmt.Printf("   -> Total Metrics:   %d\n", numRequests*payloadSize)
		fmt.Printf("   -> Throughput:      %.2f reqs/s\n", float64(numRequests)/duration.Seconds())
		fmt.Printf("   -> Peak Memory:     %.2f MB (Budget: 5.00 MB)\n", float64(finalPeak)/(1024*1024))
		
		if finalPeak > memoryBudgetBytes {
			t.Fatalf("Agent exceeded memory budget under mTLS: %.2f MB", float64(finalPeak)/(1024*1024))
		}
	})
	
	fmt.Println("=======================================================")
}
