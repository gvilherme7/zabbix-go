#!/bin/bash
set -e

NETWORK="tests-env_zbx_net"
AGENT_BIN="$(pwd)/zabbix-agent-linux"

echo "Reloading Zabbix Server and Proxy caches..."
sudo docker exec tests-env-zabbix-server-1 zabbix_server -R config_cache_reload || true
sudo docker exec tests-env-zabbix-proxy-1 zabbix_proxy -R config_cache_reload || true
sleep 5

echo "Starting 50 Chaos Engineering Nodes..."
for i in $(seq 1 50); do
  HOSTNAME="ChaosNode${i}"
  echo "Spawning ${HOSTNAME}..."
  
  # Alternate between direct server (even) and proxy (odd)
  if [ $((i%2)) -eq 0 ]; then
    SERVER="zabbix-server:10051"
  else
    SERVER="zabbix-proxy:10051"
  fi

  # Spin up container that runs the agent and a CPU stressor in background
  sudo docker run -d --name ${HOSTNAME} --net $NETWORK \
    -v $AGENT_BIN:/usr/local/bin/zabbix-agent \
    alpine sh -c "apk add --no-cache stress-ng && stress-ng --cpu 2 --vm 1 --vm-bytes 100M --timeout 600s & /usr/local/bin/zabbix-agent -mode active -server ${SERVER} -host ${HOSTNAME} -interval 2 -compress=true" > /dev/null
done

echo "Chaos unleashed. 50 containers are now heavily stressing CPU/Mem and spamming metrics every 2 seconds via active checks."
