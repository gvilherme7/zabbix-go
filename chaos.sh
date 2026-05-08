#!/bin/bash

echo "Starting OS-Level Chaos Environment..."

# 1. CPU Hogs: Run infinite loops to pin CPU cores
echo "Pinning 4 CPU cores..."
for i in {1..4}; do
    while true; do :; done &
    echo $! >> chaos_pids.txt
done

# 2. Memory Hog: Python script to constantly allocate/free hundreds of MBs of RAM
echo "Starting Memory Hog..."
python3 -c "
import time
while True:
    a = [' ' * 10**6 for _ in range(500)] # 500MB
    time.sleep(0.1)
    a = [] # Free it to force OS paging and GC thrashing
" &
echo $! >> chaos_pids.txt

# 3. Network/Bandwidth Hog: Saturate loopback interface with /dev/urandom garbage
echo "Starting Network Hog..."
nc -l -p 10055 > /dev/null &
echo $! >> chaos_pids.txt
sleep 1
while true; do cat /dev/urandom | nc 127.0.0.1 10055; done &
echo $! >> chaos_pids.txt

echo "Chaos environment is now live."
