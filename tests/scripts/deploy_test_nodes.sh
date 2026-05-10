#!/bin/bash
set -e

NETWORK="tests-env_zbx_net"
AGENT_BIN="$(pwd)/zabbix-agent-linux"

echo "[*] Deploying Node 1: Direct to Zabbix Server (Active Mode)"
sudo docker run -d --name agent-node1 --net $NETWORK \
  -v $AGENT_BIN:/usr/local/bin/zabbix-agent \
  alpine /usr/local/bin/zabbix-agent -mode active -server zabbix-server:10051 -host Node1 -interval 5

echo "[*] Deploying Node 2: Direct to Zabbix Proxy (Active Mode with Compression)"
sudo docker run -d --name agent-node2 --net $NETWORK \
  -v $AGENT_BIN:/usr/local/bin/zabbix-agent \
  alpine /usr/local/bin/zabbix-agent -mode active -server zabbix-proxy:10051 -host Node2 -interval 5 -compress=true

echo "[*] Deploying Node 3: Failover Test (Dummy -> Zabbix Server)"
sudo docker run -d --name agent-node3 --net $NETWORK \
  -v $AGENT_BIN:/usr/local/bin/zabbix-agent \
  alpine /usr/local/bin/zabbix-agent -mode active -server 127.0.0.1:1234,zabbix-server:10051 -host Node3 -interval 5

echo "[*] All nodes deployed successfully!"
sudo docker ps --filter "name=agent-node"
