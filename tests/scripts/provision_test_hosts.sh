#!/bin/bash
set -e
API="http://127.0.0.1:8080/api_jsonrpc.php"
TOKEN=$(curl -s -X POST "$API" -H "Content-Type: application/json-rpc" \
  -d '{"jsonrpc":"2.0","method":"user.login","params":{"username":"Admin","password":"zabbix"},"id":1}' | jq -r .result)
echo "Token: $TOKEN"

call() {
  curl -s -X POST "$API" -H "Content-Type: application/json-rpc" -d "$1"
}

# Host group
GROUP_RESP=$(call '{"jsonrpc":"2.0","method":"hostgroup.create","params":{"name":"Agent Validation"},"auth":"'"$TOKEN"'","id":2}')
echo "Group: $GROUP_RESP"
GROUPID=$(echo "$GROUP_RESP" | jq -r '.result.groupids[0] // empty')
if [ -z "$GROUPID" ]; then
  GROUPID=$(call '{"jsonrpc":"2.0","method":"hostgroup.get","params":{"filter":{"name":"Agent Validation"}},"auth":"'"$TOKEN"'","id":2}' | jq -r '.result[0].groupid')
fi
echo "GroupID: $GROUPID"

create_host() {
  local NAME=$1
  RESP=$(call '{"jsonrpc":"2.0","method":"host.create","params":{"host":"'"$NAME"'","groups":[{"groupid":"'"$GROUPID"'"}],"interfaces":[]},"auth":"'"$TOKEN"'","id":3}')
  HOSTID=$(echo "$RESP" | jq -r '.result.hostids[0] // empty')
  if [ -z "$HOSTID" ]; then
    HOSTID=$(call '{"jsonrpc":"2.0","method":"host.get","params":{"filter":{"host":["'"$NAME"'"]}},"auth":"'"$TOKEN"'","id":3}' | jq -r '.result[0].hostid')
  fi
  echo "$HOSTID"
}

create_item() {
  local HOSTID=$1 KEY=$2 TYPE=$3 VALUETYPE=$4 NAME=$5
  call '{"jsonrpc":"2.0","method":"item.create","params":{"hostid":"'"$HOSTID"'","name":"'"$NAME"'","key_":"'"$KEY"'","type":'"$TYPE"',"value_type":'"$VALUETYPE"',"delay":"2s"},"auth":"'"$TOKEN"'","id":4}' > /tmp/item_resp.json
  cat /tmp/item_resp.json
  echo ""
}

TRAPPER_HOST=$(create_host "TrapperHost")
echo "TrapperHost id: $TRAPPER_HOST"
for kv in "agent.ping:3" "golang.goroutines:3" "golang.mem.alloc:3" "system.cpu.load:0" "vm.memory.size.total:3" "app.status:1" "app.users:3"; do
  KEY=${kv%%:*}; VT=${kv##*:}
  create_item "$TRAPPER_HOST" "$KEY" 2 "$VT" "$KEY (trapper)"
done

ACTIVE_HOST=$(create_host "ActiveHost")
echo "ActiveHost id: $ACTIVE_HOST"
for kv in "agent.ping:3" "golang.goroutines:3" "system.cpu.load:0" "vm.memory.size.total:3" "app.status:1" "app.users:3"; do
  KEY=${kv%%:*}; VT=${kv##*:}
  create_item "$ACTIVE_HOST" "$KEY" 7 "$VT" "$KEY (active)"
done

echo "DONE"
echo "TRAPPER_HOST=$TRAPPER_HOST"
echo "ACTIVE_HOST=$ACTIVE_HOST"
echo "TOKEN=$TOKEN"
