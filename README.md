# Custom Lightweight Zabbix Agent

A custom, ultra-lightweight, and zero-dependency Zabbix Agent alternative written in Go. Its purpose is to consume almost no memory while supporting robust telemetry delivery.

### Key Features
- **Tiny Footprint:** Runs statically with an active heap size of less than `1 MB`, proven in highly constrained appliance testing.
- **Hybrid Modes:** Supports both classic Trapper Mode and a true Active Checks Scheduler that respects item-specific intervals.
- **UserParameters:** Parses standard `zabbix_agentd.conf` files to execute dynamic custom shell commands across all OSs.
- **Disk Buffering:** If the connection to Zabbix goes down, the agent writes the telemetry payload to a local `./metrics.dat` file safely (via atomic rename) and syncs it when the connection is restored.
- **Native Service Support:** Installs seamlessly as an unmanaged Background Service / Daemon on Linux (systemd), Windows (Service Control Manager), and macOS (launchd).
- **Nano-Proxy Mode:** Can act as an ultra-lightweight Zabbix Proxy (`-mode proxy`). It avoids SQLite dependency by using a high-throughput, append-only disk buffer for incoming agent metrics, making it perfect for Raspberry Pis and constrained edge gateways.
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
./zabbix-agent-linux -mode proxy -proxy-port 10051 -proxy-tls true -tls-cert-file proxy_server.crt -tls-key-file proxy_server.key -server 192.168.1.100:10051
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
