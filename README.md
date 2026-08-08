# Custom Lightweight Zabbix Agent

A custom, ultra-lightweight, and zero-dependency Zabbix Agent alternative written in Go. Its purpose is to consume almost no memory while supporting robust telemetry delivery.

### Key Features
- **Tiny Footprint:** Runs statically with an active heap size of less than `1 MB`, proven in highly constrained appliance testing.
- **Hybrid Modes:** Supports both classic Trapper Mode and a true Active Checks Scheduler that respects item-specific intervals.
- **UserParameters:** Parses standard `zabbix_agentd.conf` files to execute dynamic custom shell commands across all OSs, including parameterized items (`UserParameter=vfs.file.size[*],stat -c%s $1`).
- **Disk Buffering:** If the connection to Zabbix goes down, the agent writes the telemetry payload to a local `./metrics.dat` file safely (via atomic rename) and syncs it when the connection is restored. Capped at `100 MB` by default (`-buffer-max-mb`) so a prolonged outage can't fill the disk.
- **Native Service Support:** Installs seamlessly as an unmanaged Background Service / Daemon on Linux (systemd), Windows (Service Control Manager), and macOS (launchd).
- **Nano-Proxy Mode:** Can act as an ultra-lightweight Zabbix Proxy (`-mode proxy`). It avoids SQLite dependency by using a high-throughput, append-only disk buffer for incoming agent metrics, making it perfect for Raspberry Pis and constrained edge gateways.
- **Simultaneous Proxy/Agent Mode:** Can run as both an agent and a proxy simultaneously (e.g., `-mode active+proxy`), sharing memory buffers for maximum efficiency.
- **Active-Passive Redundancy & Broadcasting:** Provide a comma-separated list of servers to `-server`. The Agent natively handles failover if the primary goes down, while the Nano-Proxy will automatically broadcast downstream metrics to all configured upstream servers simultaneously.
- **Protocol Compression:** Supports native Zabbix protocol compression (`ZBXD\x03`) to drastically reduce bandwidth utilization (`-compress=true`).
- **Prometheus Self-Monitoring:** Exposes an internal HTTP `/metrics` endpoint to monitor memory usage and thread counts (`-metrics-port=8080`).
- **Native Log Monitoring & LLD:** Includes a highly efficient native log tailer and local filesystem/network interface discovery (`vfs.fs.discovery`, `net.if.discovery`).
- **Zero-Bloat OS Plugins:** Has custom `/proc` parsers and direct `syscall` mappings (Windows `kernel32.dll`) tailored to OS environments to prevent importing huge `go-psutil` dependency trees.

---

## 1. Quick Start Usage (Interactive)
You don't need to install it to use it. You can run the executable interactively in the terminal like this:

```bash
# Standard Unencrypted Run
./zabbix-agent-linux -server 192.168.1.100:10051 -host "My_Custom_Host" -mode active -interval 30

# Secure mTLS Run with AES-GCM Encrypted Local Buffer
./zabbix-agent-linux -server 192.168.1.100:10051 -tls-connect cert -tls-ca-file ca.crt -tls-cert-file client.crt -tls-key-file client.key -buffer-key "super_secret_encryption_key"

# Using a Config File for UserParameters & TLS Defaults
./zabbix-agent-linux -config /etc/zabbix/zabbix_agentd.conf

# Run as a Nano-Proxy
./zabbix-agent-linux -mode proxy -proxy-port 10051 -server 192.168.1.100:10051

# Run as a Nano-Proxy with mTLS enforced for downstream agents
# -tls-ca-file is required here: it's the CA the proxy uses to verify each
# downstream agent's client certificate (true mutual TLS, not just a TLS listener).
# Note the "=" on -proxy-tls: Go's flag package only treats a bare "-proxy-tls true"
# as the flag by itself (true) plus a stray positional argument, which stops ALL
# further flag parsing — every flag after it, including the TLS file paths, would
# silently be ignored and the proxy would fail to start.
./zabbix-agent-linux -mode proxy -proxy-port 10051 -proxy-tls=true -tls-cert-file proxy_server.crt -tls-key-file proxy_server.key -tls-ca-file downstream_ca.crt -server 192.168.1.100:10051

# Run simultaneously as Agent and Nano-Proxy with multiple fallback/broadcast servers
./zabbix-agent-linux -mode trapper+proxy -proxy-port 10051 -server 192.168.1.100:10051,192.168.1.101:10051

# Run with Zabbix Protocol Compression and a Prometheus Metrics port
./zabbix-agent-linux -server 192.168.1.100:10051 -compress=true -metrics-port=8080
```

---

## 2. Installing it as an OS Background Service
This agent includes an auto-installer that sets itself up on your OS's bootloader. 

### Step A: Install the Service
Execute the binary telling it to `install` itself and pass the arguments you want the background thread to run with permanently.

*Linux / macOS:*
```bash
sudo ./zabbix-agent -service install -server 192.168.1.100:10051 -host "Prod-Web-01" -mode active -interval 30
```
*Windows Command Prompt (Run as Administrator):*
```cmd
zabbix-agent.exe -service install -server 192.168.1.100:10051 -host "Prod-Web-01" -mode active -interval 30
```

### Step B: Start or Stop the Service
Once installed, use the agent's control commands to manage the daemon state.
```bash
# Start the agent running in the background
./zabbix-agent -service start

# Stop the agent gracefully
./zabbix-agent -service stop

# Uninstall and completely wipe the service from your OS
./zabbix-agent -service uninstall
```

---

## 3. How to Compile for Any OS (Zero Dependencies)
You only need to compile this codebase ONCE. Use these commands to build the statically-linked, zero-dependency binaries that you can just copy-paste to your target servers.

### Compile for Linux (64-bit)
```bash
env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -ldflags '-extldflags "-static" -s -w' -o zabbix-agent-linux
```

### Compile for Windows (64-bit)
```bash
env CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -a -ldflags '-s -w' -o zabbix-agent-windows.exe
```

### Compile for macOS (Apple Silicon / M1 / M2)
```bash
env CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -a -ldflags '-s -w' -o zabbix-agent-macos-arm64
```

### Compile for macOS (Intel)
```bash
env CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -a -ldflags '-s -w' -o zabbix-agent-macos-intel
```

---

## 4. Documentation & Benchmarks
This repository contains extensive benchmarking and security reports verifying the agent's readiness for production:
- [TESTING.md](TESTING.md): Details the stress test suites, memory constraints testing (5MB budget), and a direct benchmark comparison against the official `zabbix_agentd` (C) and `zabbix_agent2` (Go).
- [SECURITY.md](SECURITY.md): Details the MITM (Man-in-the-Middle) packet sniffer testing, OS-level Chaos Engineering resilience, and rogue-server validation tests for mTLS.

---

## 5. Zabbix Module & Server Compatibility

The Lightweight Agent and Nano-Proxy are designed to act as true "drop-in" replacements. They implement the strict `ZBXD\x01` TCP protocol header and exact JSON schema expected by the official Zabbix Server.

### Server Version Compatibility
Fully compatible with all modern and legacy Zabbix Server LTS releases, thanks to our strict adherence to the foundational `ZBXD\x01` protocol:
- **Zabbix 3.0 LTS** *(Legacy)*
- **Zabbix 4.0 LTS** *(Legacy)*
- **Zabbix 5.0 LTS**
- **Zabbix 6.0 LTS**
- **Zabbix 7.0 LTS**

### Module & Ecosystem Compatibility
Because the protocol is perfectly emulated, no modifications to your Zabbix Server are required. 
- **Standard Templates**: 100% compatible with standard Zabbix OS templates (Linux, Windows, macOS) for the keys it natively supports.
- **Low-Level Discovery (LLD)**: Fully supports returning JSON arrays for dynamic LLD rule creation.
- **Dependent Items**: Works natively with dependent items and bulk data collection.
- **UserParameters**: By pointing the agent to your existing `zabbix_agentd.conf`, all your existing custom shell scripts, database modules, and bash integrations will execute exactly as they do under the official agent.
- **Zabbix Sender Compatibility**: The Nano-Proxy perfectly mimics Zabbix Server ingestion, meaning official `zabbix_sender` utilities (and any third-party sender scripts) can push data to our Nano-Proxy on port `10051` seamlessly.

---

## 6. Dependencies & Fonts
This project fiercely guards its memory footprint and cross-compilation simplicity by avoiding heavy dependency trees, CGO, or external fonts. 

The **only** external dependency used is:
- `github.com/kardianos/service` - Used to provide native, cross-platform OS background service/daemon installations across Linux (systemd), macOS (launchd), and Windows.
