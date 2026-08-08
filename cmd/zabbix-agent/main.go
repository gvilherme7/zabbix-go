package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gvilherme7/zabbix-go/internal/agent"
	"github.com/gvilherme7/zabbix-go/internal/buffer"
	"github.com/gvilherme7/zabbix-go/internal/config"
	"github.com/gvilherme7/zabbix-go/internal/metrics"
	"github.com/gvilherme7/zabbix-go/internal/proxy"
	"github.com/gvilherme7/zabbix-go/internal/zabbix"
	"github.com/kardianos/service"
)

type program struct {
	exit        chan struct{}
	done        chan struct{}
	server      string
	host        string
	interval    int
	mode        string
	proxyPort   int
	proxyTLS    bool
	tlsCertFile string
	tlsKeyFile  string
	tlsCAFile   string
	bufferKey   string
	bufferMaxMB int
	metricsPort int
	compress    bool
	tlsConf     *tls.Config
}

func (p *program) Start(s service.Service) error {
	p.exit = make(chan struct{})
	p.done = make(chan struct{})
	go p.run()
	return nil
}

func (p *program) run() {
	defer close(p.done)

	// Setup Disk Buffer Encryption Key if provided
	if p.bufferKey != "" {
		hash := sha256.Sum256([]byte(p.bufferKey))
		buffer.AesKey = hash[:]
	}
	buffer.MaxBufferBytes = int64(p.bufferMaxMB) * 1024 * 1024

	metrics.StartPrometheusServer(p.metricsPort)

	isProxy := strings.Contains(p.mode, "proxy")
	isAgent := strings.Contains(p.mode, "trapper") || strings.Contains(p.mode, "active")

	var proxyTLSConf *tls.Config
	if isProxy && p.proxyTLS {
		cert, err := tls.LoadX509KeyPair(p.tlsCertFile, p.tlsKeyFile)
		if err != nil {
			log.Fatalf("Proxy TLS failed to load cert: %v", err)
		}

		// Security: require and verify a client certificate from downstream
		// agents (true mTLS). Without ClientCAs/ClientAuth, tls.Config accepts
		// any TLS client regardless of certificate, which does not match the
		// documented "mTLS enforced for downstream agents" behavior.
		if p.tlsCAFile == "" {
			log.Fatalf("Proxy TLS requires -tls-ca-file to verify downstream agent client certificates")
		}
		caCert, err := os.ReadFile(p.tlsCAFile)
		if err != nil {
			log.Fatalf("Proxy TLS failed to read CA file: %v", err)
		}
		clientCAPool := x509.NewCertPool()
		if !clientCAPool.AppendCertsFromPEM(caCert) {
			log.Fatalf("Proxy TLS CA file contains no valid certificates")
		}

		proxyTLSConf = &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientCAs:    clientCAPool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		}
	}

	if isProxy {
		var proxyClients []*zabbix.Client
		for _, s := range strings.Split(p.server, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				proxyClients = append(proxyClients, zabbix.NewClient(s, p.tlsConf != nil, p.tlsConf, p.compress))
			}
		}

		srv := &proxy.Server{
			Clients:    proxyClients,
			ListenAddr: fmt.Sprintf(":%d", p.proxyPort),
			UseTLS:     p.proxyTLS,
			TLSConfig:  proxyTLSConf,
			ExitChan:   p.exit,
		}
		if isAgent {
			go srv.Start()
		} else {
			srv.Start()
			return
		}
	}

	if isAgent {
		client := zabbix.NewClient(p.server, p.tlsConf != nil, p.tlsConf, p.compress)

		agentMode := "trapper"
		if strings.Contains(p.mode, "active") {
			agentMode = "active"
		}

		agt := &agent.Agent{
			Client:   client,
			Server:   p.server,
			Host:     p.host,
			Interval: p.interval,
			Mode:     agentMode,
			ExitChan: p.exit,
		}
		agt.Start()
	}
}

func (p *program) Stop(s service.Service) error {
	close(p.exit)
	<-p.done
	return nil
}

// serviceArgs returns the command-line arguments to persist for the
// installed service, forwarding everything the operator passed except the
// -service flag itself (and its value, e.g. "install") — that flag controls
// this one-off invocation, not what the running service should do.
func serviceArgs() []string {
	var args []string
	skipNext := false
	for _, a := range os.Args[1:] {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "-service" || a == "--service" {
			skipNext = true
			continue
		}
		if strings.HasPrefix(a, "-service=") || strings.HasPrefix(a, "--service=") {
			continue
		}
		args = append(args, a)
	}
	return args
}

func main() {
	svcFlag := flag.String("service", "", "Control the system service: 'install', 'uninstall', 'start', 'stop', 'restart'")
	server := flag.String("server", "127.0.0.1:10051", "Zabbix Server IP:Port (comma-separated for failover/broadcast)")
	host := flag.String("host", "", "Hostname in Zabbix")
	interval := flag.Int("interval", 60, "Send interval in seconds (Trapper mode)")
	mode := flag.String("mode", "trapper", "Mode: 'trapper', 'active', 'proxy', 'trapper+proxy', or 'active+proxy'")
	configPath := flag.String("config", "", "Path to zabbix_agentd.conf (for UserParameters)")
	tlsConnect := flag.String("tls-connect", "unencrypted", "How to connect: 'unencrypted', 'cert'")
	tlsCAFile := flag.String("tls-ca-file", "", "Path to CA file")
	tlsCertFile := flag.String("tls-cert-file", "", "Path to TLS Certificate")
	tlsKeyFile := flag.String("tls-key-file", "", "Path to TLS Key")
	proxyPort := flag.Int("proxy-port", 10051, "Port to listen on when in proxy mode")
	proxyTLS := flag.Bool("proxy-tls", false, "Use TLS for incoming proxy connections (requires tls-cert-file and tls-key-file)")
	bufferKey := flag.String("buffer-key", "", "If provided, enables AES-GCM encryption for local disk buffers")
	bufferMaxMB := flag.Int("buffer-max-mb", 100, "Max size in MB for the local disk buffer file before new metrics are dropped instead of growing it further (0 disables the cap)")
	metricsPort := flag.Int("metrics-port", 0, "Port to expose Prometheus /metrics (0 to disable)")
	compress := flag.Bool("compress", false, "Enable Zabbix protocol compression (ZBXD\\x03)")
	flag.Parse()

	if *configPath != "" {
		configParams, err := config.ParseConfig(*configPath)
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
	if *tlsConnect == "cert" {
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

	if *host == "" {
		h, err := os.Hostname()
		if err == nil {
			*host = h
		} else {
			*host = "Unknown"
		}
	}

	prg := &program{
		server:      *server,
		host:        *host,
		interval:    *interval,
		mode:        *mode,
		proxyPort:   *proxyPort,
		proxyTLS:    *proxyTLS,
		tlsCertFile: *tlsCertFile,
		tlsKeyFile:  *tlsKeyFile,
		tlsCAFile:   *tlsCAFile,
		bufferKey:   *bufferKey,
		bufferMaxMB: *bufferMaxMB,
		metricsPort: *metricsPort,
		compress:    *compress,
		tlsConf:     tlsConf,
	}

	svcConfig := &service.Config{
		Name:        "zabbix-agent-lightweight",
		DisplayName: "Zabbix Agent (Lightweight Custom)",
		Description: "A super lightweight standalone Zabbix Agent data pusher.",
		// Forward every flag the operator passed to "-service install", not a
		// hardcoded subset — TLS, mTLS, buffer encryption, compression, and
		// -config were previously dropped from the installed service
		// entirely, meaning a service installed with e.g. -buffer-key or
		// -tls-connect cert would silently run without them on every future
		// start/reboot.
		Arguments: serviceArgs(),
	}

	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	if *svcFlag != "" {
		if err := service.Control(s, *svcFlag); err != nil {
			log.Fatalf("Failed to execute %s service command: %v", *svcFlag, err)
		}
		log.Printf("Service command [%s] succeeded.", *svcFlag)
		return
	}

	if err := s.Run(); err != nil {
		log.Fatal(err)
	}
}
