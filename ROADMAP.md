# Roadmap: Lightweight Advanced Zabbix Agent & Proxy

This document outlines the development phases, completed milestones, and future goals for the Zero-Dependency Lightweight Go Zabbix Agent project.

## Phase 1: Core Foundation (COMPLETED)
- [x] Establish zero-dependency Go architecture.
- [x] Implement memory-mapped AES-GCM encrypted disk buffering (`internal/buffer`).
- [x] Develop TCP Zabbix Sender protocol (`ZBXD\x01` and `ZBXD\x03` compression).

## Phase 2: Security & Encryption (COMPLETED)
- [x] Implement robust TLS 1.3 architecture with mTLS and PSK support.
- [x] Develop strict memory limits, OOM protection, and Slowloris mitigation (`Timeout`).
- [x] Containerize the ecosystem and run baseline network encryption tests.

## Phase 3: Active Monitoring & Extensibility (COMPLETED)
- [x] Implement the Zabbix "Active Checks" protocol mapping (`agent data` requests).
- [x] Integrate Linux native system plugins without CGO (e.g., `/proc` memory, CPU loading).
- [x] Establish a generic `UserParameter` parser mapping to `zabbix_agentd.conf`.
- [x] Build automated Proxy failover handling (`Server=ip1,ip2`).

## Phase 4: Hardening & Enterprise Protocol Compliance (COMPLETED)
- [x] Conduct 50-node multi-container Chaos Engineering.
- [x] Prove memory stability during high-volume Goroutine spawns.
- [x] Refactor Session IDs for uniqueness to prevent Zabbix Active Checks flood-dropping.
- [x] Implement complete LTS manual Zabbix protocol backwards compatibility (e.g., proper reporting of `ZBX_NOTSUPPORTED` states).

## Phase 5: Future Enhancements (ROADMAP)
- [ ] **WASM Plugin Architecture**: Integrate WebAssembly for dynamic, sandboxed metric gathering without recompiling the main binary.
- [ ] **Log Tailing Engine**: Finish implementing the `inode` tracking logic for enterprise log streaming.
- [ ] **StatsD Integration**: Expand the lightweight listener to ingest UDP StatsD metrics natively.
- [ ] **Daemonization Enhancements**: Integrate fully with Windows Services and `systemd` socket activation natively.
