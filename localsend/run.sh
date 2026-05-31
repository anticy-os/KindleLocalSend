#!/bin/sh
cd /mnt/us/extensions/localsend
PIDFILE="/var/run/localsend.pid"

case "$1" in
start)
    iptables -F INPUT
    iptables -P INPUT ACCEPT
    # Wait for wlan0 to be fully up before binding multicast
    sleep 3
    /mnt/us/extensions/localsend/bin/localsendd > /tmp/localsend.log 2>&1 &
    echo $! > "$PIDFILE"
    ;;
stop)
    if [ -f "$PIDFILE" ]; then
        kill "$(cat "$PIDFILE")"
        rm "$PIDFILE"
    fi
    ;;
esac