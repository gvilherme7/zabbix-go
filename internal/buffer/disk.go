package buffer

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/gvilherme7/zabbix-go/internal/zabbix"
)

var (
	GlobalMutex sync.Mutex
	AesKey      []byte
)

// Generate a 12-byte nonce
func generateNonce() ([]byte, error) {
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return nonce, nil
}

func encryptLine(data []byte) []byte {
	if len(AesKey) == 0 {
		return data
	}
	block, err := aes.NewCipher(AesKey)
	if err != nil {
		return data // Fallback to plaintext if key is invalid, though it should be validated earlier
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return data
	}
	nonce, err := generateNonce()
	if err != nil {
		return data
	}
	ciphertext := aesgcm.Seal(nil, nonce, data, nil)
	// Prepend nonce to ciphertext
	return append(nonce, ciphertext...)
}

func decryptLine(data []byte) []byte {
	if len(AesKey) == 0 {
		return data
	}
	if len(data) < 12 {
		return data
	}
	block, err := aes.NewCipher(AesKey)
	if err != nil {
		return data
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return data
	}
	nonce := data[:12]
	ciphertext := data[12:]
	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil // Invalid or corrupted data
	}
	return plaintext
}

func WriteMetrics(filePath string, metrics []zabbix.Metric) error {
	GlobalMutex.Lock()
	defer GlobalMutex.Unlock()

	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, m := range metrics {
		line, _ := json.Marshal(m)
		line = encryptLine(line)
		file.Write(line)
		file.Write([]byte("\n"))
	}
	return nil
}

// Flush returns the metrics that were buffered, and deletes the processing file
func Flush(filePath string) []zabbix.Metric {
	GlobalMutex.Lock()
	info, err := os.Stat(filePath)
	if err != nil || info.Size() == 0 {
		GlobalMutex.Unlock()
		return nil
	}

	processingPath := filePath + ".processing"
	err = os.Rename(filePath, processingPath)
	GlobalMutex.Unlock()

	if err != nil {
		return nil
	}
	defer os.Remove(processingPath)

	data, err := os.ReadFile(processingPath)
	if err != nil || len(data) == 0 {
		return nil
	}

	var metrics []zabbix.Metric
	lines := bytes.Split(data, []byte("\n"))
	for _, l := range lines {
		if len(strings.TrimSpace(string(l))) == 0 {
			continue
		}
		
		decrypted := decryptLine(l)
		if decrypted == nil {
			log.Println("[Buffer] Warning: Failed to decrypt a buffered metric. Corruption or key mismatch.")
			continue
		}

		var m zabbix.Metric
		if err := json.Unmarshal(decrypted, &m); err == nil {
			metrics = append(metrics, m)
		}
	}
	return metrics
}
