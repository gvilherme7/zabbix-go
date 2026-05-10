#!/bin/bash
set -e

# Reload Zabbix Server cache
sudo docker exec tests-env-zabbix-server-1 zabbix_server -R config_cache_reload

cat << 'EOF' > zabbix_agentd.conf
UserParameter=app.status,curl -s http://127.0.0.1:8080/status.json | grep -o '"status":"[^"]*"' | cut -d'"' -f4 || echo "DOWN"
UserParameter=app.users,curl -s http://127.0.0.1:8080/status.json | grep -o '"users":[^,}]*' | cut -d':' -f2 || echo "0"
EOF

# Deploy container
sudo docker rm -f AppNode1 || true
sudo docker run -d --name AppNode1 --net tests-env_zbx_net \
  -v $(pwd)/zabbix-agent-linux:/usr/local/bin/zabbix-agent \
  -v $(pwd)/zabbix_agentd.conf:/etc/zabbix_agentd.conf \
  python:3.9-alpine sh -c "apk add --no-cache curl && echo '{\"status\":\"UP\", \"users\": 42}' > /tmp/status.json && cd /tmp && python3 -m http.server 8080 & sleep 2 && /usr/local/bin/zabbix-agent -mode active -server zabbix-server:10051 -host AppNode1 -config /etc/zabbix_agentd.conf -interval 2"

echo "AppNode1 deployed successfully with custom UserParameters!"
