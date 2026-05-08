package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/service"
)

type Metric struct {
	Host  string `json:"host"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ZabbixPacket struct {
	Request string   `json:"request"`
	Data    []Metric `json:"data"`
}

type ActiveCheckRequest struct {
	Request string `json:"request"`
	Host    string `json:"host"`
}

type ActiveCheckResponse struct {
	Response string `json:"response"`
	Data     []struct {
		Key   string `json:"key"`
		Delay string `json:"delay"`
	} `json:"data"`
}

var (
	bufferFilePath = "metrics.dat"
	bufferMutex    sync.Mutex
	globalClient   *ZabbixClient
)

type ZabbixClient struct {
	serverAddr string
	tlsConfig  *tls.Config
	useTLS     bool
	conn       net.Conn
	mu         sync.Mutex
}

func (zc *ZabbixClient) doReq(jsonData []byte) ([]byte, error) {
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

type program struct {
	exit     chan struct{}
	done     chan struct{}
	server   string
	host     string
	interval int
	mode     string
}

func (p *program) Start(s service.Service) error {
	p.exit = make(chan struct{})
	p.done = make(chan struct{})
	// Start should not block. Do the actual work async.
	go p.run()
	return nil
}

func (p *program) run() {
	defer close(p.done)
	log.Printf("Lightweight Advanced Zabbix Agent service started. Mode: [%s]", p.mode)

	if p.mode == "active" {
		p.runActiveScheduler()
		return
	}

	// Trapper mode (global interval push)
	ticker := time.NewTicker(time.Duration(p.interval) * time.Second)
	defer ticker.Stop()

	runCycle(p.mode, p.server, p.host)

	for {
		select {
		case <-ticker.C:
			runCycle(p.mode, p.server, p.host)
		case <-p.exit:
			log.Println("Service stop requested by OS. Doing graceful teardown & buffer flushing...")
			flushDiskBuffer(p.server, p.host)
			return
		}
	}
}

func (p *program) Stop(s service.Service) error {
	// Signal loops to quit
	close(p.exit)
	<-p.done // Wait for graceful shutdown
	return nil
}

func main() {
	svcFlag := flag.String("service", "", "Control the system service: 'install', 'uninstall', 'start', 'stop', 'restart'")
	server := flag.String("server", "127.0.0.1:10051", "Zabbix Server IP:Port")
	host := flag.String("host", "", "Hostname in Zabbix")
	interval := flag.Int("interval", 60, "Send interval in seconds (Trapper mode)")
	mode := flag.String("mode", "trapper", "Mode: 'trapper' or 'active'")
	configPath := flag.String("config", "", "Path to zabbix_agentd.conf (for UserParameters)")
	tlsConnect := flag.String("tls-connect", "unencrypted", "How to connect: 'unencrypted', 'cert'")
	tlsCAFile := flag.String("tls-ca-file", "", "Path to CA file")
	tlsCertFile := flag.String("tls-cert-file", "", "Path to TLS Certificate")
	tlsKeyFile := flag.String("tls-key-file", "", "Path to TLS Key")
	flag.Parse()

	var configParams ConfigParams
	if *configPath != "" {
		var err error
		configParams, err = ParseConfig(*configPath)
		if err != nil {
			log.Fatalf("Failed to parse config file: %v", err)
		}
		if *tlsConnect == "unencrypted" && configParams.TLSConnect != "" {
			*tlsConnect = configParams.TLSConnect
		}
		if *tlsCAFile == "" && configParams.TLSCAFile != "" {
			*tlsCAFile = configParams.TLSCAFile
		}
		if *tlsCertFile == "" && configParams.TLSCertFile != "" {
			*tlsCertFile = configParams.TLSCertFile
		}
		if *tlsKeyFile == "" && configParams.TLSKeyFile != "" {
			*tlsKeyFile = configParams.TLSKeyFile
		}
	}

	var tlsConf *tls.Config
	useTLS := *tlsConnect == "cert"
	if useTLS {
		cert, err := tls.LoadX509KeyPair(*tlsCertFile, *tlsKeyFile)
		if err != nil {
			log.Fatalf("Failed to load TLS cert/key: %v", err)
		}
		caCert, err := os.ReadFile(*tlsCAFile)
		if err != nil {
			log.Fatalf("Failed to read CA file: %v", err)
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)

		tlsConf = &tls.Config{
			Certificates:       []tls.Certificate{cert},
			RootCAs:            caCertPool,
			ClientSessionCache: tls.NewLRUClientSessionCache(32),
		}
	}

	globalClient = &ZabbixClient{
		serverAddr: *server,
		useTLS:     useTLS,
		tlsConfig:  tlsConf,
	}

	if *host == "" {
		h, err := os.Hostname()
		if err == nil {
			*host = h
		} else {
			*host = "Unknown"
		}
	}

	prg := &program{
		server:   *server,
		host:     *host,
		interval: *interval,
		mode:     *mode,
	}

	svcConfig := &service.Config{
		Name:        "zabbix-agent-lightweight",
		DisplayName: "Zabbix Agent (Lightweight Custom)",
		Description: "A super lightweight standalone Zabbix Agent data pusher.",
		// Inject the exact arguments passed during install to the final background service executable
		Arguments: []string{
			fmt.Sprintf("-server=%s", *server),
			fmt.Sprintf("-host=%s", *host),
			fmt.Sprintf("-interval=%d", *interval),
			fmt.Sprintf("-mode=%s", *mode),
		},
	}

	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	if *svcFlag != "" {
		err = service.Control(s, *svcFlag)
		if err != nil {
			log.Fatalf("Failed to execute %s service command: %v", *svcFlag, err)
		}
		log.Printf("Service command [%s] succeeded.", *svcFlag)
		return
	}

	// Blocks running the service
	err = s.Run()
	if err != nil {
		log.Fatal(err)
	}
}

func runCycle(mode, server, host string) {
	var keysToCollect []string
	if mode == "active" {
		activeKeys, err := fetchActiveChecks(server, host)
		if err != nil {
			log.Printf("Failed to fetch active checks config: %v", err)
			return
		}
		keysToCollect = activeKeys
	} else {
		for k := range Registry {
			keysToCollect = append(keysToCollect, k)
		}
	}
	collectKeysAndSend(server, host, keysToCollect)
}

type activeItem struct {
	key   string
	delay int
}

func parseDelay(d string) int {
	if d == "" {
		return 60
	}
	if strings.HasSuffix(d, "s") || strings.HasSuffix(d, "m") || strings.HasSuffix(d, "h") {
		if dur, err := time.ParseDuration(d); err == nil {
			return int(dur.Seconds())
		}
	}
	sec, _ := strconv.Atoi(d)
	if sec <= 0 {
		return 60
	}
	return sec
}

func (p *program) runActiveScheduler() {
	log.Println("Starting Active Checks Scheduler...")
	var wg sync.WaitGroup
	defer wg.Wait()

	schedule := make(map[string]time.Time)
	items := make(map[string]activeItem)

	refreshTicker := time.NewTicker(120 * time.Second)
	defer refreshTicker.Stop()

	if newItems, err := fetchActiveChecksParsed(p.server, p.host); err == nil {
		updateActiveSchedule(newItems, &items, &schedule)
	} else {
		log.Printf("Initial active checks fetch failed: %v", err)
	}

	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-refreshTicker.C:
			if newItems, err := fetchActiveChecksParsed(p.server, p.host); err == nil {
				updateActiveSchedule(newItems, &items, &schedule)
			}
		case now := <-tick.C:
			var due []string
			for key, nextTime := range schedule {
				if now.After(nextTime) || now.Equal(nextTime) {
					due = append(due, key)
					item := items[key]
					schedule[key] = now.Add(time.Duration(item.delay) * time.Second)
				}
			}
			if len(due) > 0 {
				wg.Add(1)
				go func(keys []string) {
					defer wg.Done()
					collectKeysAndSend(p.server, p.host, keys)
				}(due)
			}
		case <-p.exit:
			log.Println("Service stop requested by OS. Doing graceful teardown & buffer flushing...")
			flushDiskBuffer(p.server, p.host)
			return
		}
	}
}

func updateActiveSchedule(newItems []activeItem, items *map[string]activeItem, schedule *map[string]time.Time) {
	currentKeys := make(map[string]bool)
	for _, item := range newItems {
		currentKeys[item.key] = true
		if existing, exists := (*items)[item.key]; !exists || existing.delay != item.delay {
			(*items)[item.key] = item
			(*schedule)[item.key] = time.Now() // Schedule immediately
		}
	}
	for k := range *schedule {
		if !currentKeys[k] {
			delete(*schedule, k)
			delete(*items, k)
		}
	}
}

func fetchActiveChecksParsed(serverAddr, hostName string) ([]activeItem, error) {
	req := ActiveCheckRequest{
		Request: "active checks",
		Host:    hostName,
	}
	data, _ := json.Marshal(req)

	resp, err := globalClient.doReq(data)
	if err != nil {
		return nil, err
	}

	var parsed ActiveCheckResponse
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return nil, fmt.Errorf("active checks schema decode failure: %v", err)
	}

	var items []activeItem
	for _, item := range parsed.Data {
		items = append(items, activeItem{
			key:   item.Key,
			delay: parseDelay(item.Delay),
		})
	}
	return items, nil
}

func fetchActiveChecks(serverAddr, hostName string) ([]string, error) {
	req := ActiveCheckRequest{
		Request: "active checks",
		Host:    hostName,
	}
	data, _ := json.Marshal(req)

	resp, err := globalClient.doReq(data)
	if err != nil {
		return nil, err
	}

	var parsed ActiveCheckResponse
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return nil, fmt.Errorf("active checks schema decode failure: %v", err)
	}

	var keys []string
	for _, item := range parsed.Data {
		keys = append(keys, item.Key)
	}
	return keys, nil
}

func collectKeysAndSend(serverAddr, hostName string, keys []string) {
	var metrics []Metric
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, k := range keys {
		plugin, exists := Registry[k]
		if !exists {
			continue
		}
		wg.Add(1)
		go func(key string, plug Plugin) {
			defer wg.Done()

			ch := make(chan struct {
				val string
				err error
			}, 1)

			go func() {
				val, err := plug.Collect()
				ch <- struct {
					val string
					err error
				}{val, err}
			}()

			select {
			case res := <-ch:
				if res.err == nil {
					mu.Lock()
					metrics = append(metrics, Metric{Host: hostName, Key: key, Value: res.val})
					mu.Unlock()
				}
			case <-time.After(5 * time.Second):
				log.Printf("Plugin [%s] timed out after 5 seconds", key)
			}
		}(k, plugin)
	}
	wg.Wait()

	if err := sendData(serverAddr, hostName, metrics); err != nil {
		log.Printf("Send Network Warning: %v (Metrics stored to disk buffer)", err)
	}
}

func sendData(serverAddr, hostName string, metrics []Metric) error {
	if len(metrics) == 0 {
		return nil
	}
	packet := ZabbixPacket{
		Request: "sender data",
		Data:    metrics,
	}
	jsonData, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	_, err = globalClient.doReq(jsonData)
	if err != nil {
		bufferToDisk(metrics)
		return err
	}
	// log.Printf("Sent %d metrics.", len(metrics))
	flushDiskBuffer(serverAddr, hostName)
	return nil
}

func bufferToDisk(metrics []Metric) {
	bufferMutex.Lock()
	defer bufferMutex.Unlock()
	file, err := os.OpenFile(bufferFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	for _, m := range metrics {
		line, _ := json.Marshal(m)
		file.Write(line)
		file.Write([]byte("\n"))
	}
}

func flushDiskBuffer(serverAddr, hostName string) {
	bufferMutex.Lock()
	info, err := os.Stat(bufferFilePath)
	if err != nil || info.Size() == 0 {
		bufferMutex.Unlock()
		return
	}

	processingPath := bufferFilePath + ".processing"
	err = os.Rename(bufferFilePath, processingPath)
	bufferMutex.Unlock()

	if err != nil {
		return
	}
	defer os.Remove(processingPath)

	data, err := os.ReadFile(processingPath)
	if err != nil || len(data) == 0 {
		return
	}

	var metrics []Metric
	lines := bytes.Split(data, []byte("\n"))
	for _, l := range lines {
		if len(l) == 0 {
			continue
		}
		var m Metric
		if err := json.Unmarshal(l, &m); err == nil {
			metrics = append(metrics, m)
		}
	}
	if len(metrics) > 0 {
		log.Printf("[Buffer] Flushing %d stacked metrics back to server...", len(metrics))
		sendDataRaw(serverAddr, metrics)
	}
}

func sendDataRaw(serverAddr string, metrics []Metric) {
	packet := ZabbixPacket{
		Request: "sender data",
		Data:    metrics,
	}
	jsonData, _ := json.Marshal(packet)
	if _, err := globalClient.doReq(jsonData); err != nil {
		bufferToDisk(metrics)
	}
}