#!/bin/sh
# A home router: forwards its LAN to the "internet" network and masquerades.
# NAT=cone (default): Linux keeps a socket's source port for every
#   destination, so the mapping the server saw is the one a peer can reach
#   once this side has punched. Hole punching works.
# NAT=symmetric: a fresh random port per destination, so the mapping the
#   server saw is useless to a peer. Hole punching fails; this needs a relay.
set -e
ip route del default 2>/dev/null || true   # the lab's subnets only, never the real internet
WAN=$(ip -o -4 addr show | awk '$4 ~ /^10\.99\./ {print $2}')
if [ "$NAT" = symmetric ]; then
  iptables -t nat -A POSTROUTING -o "$WAN" -j MASQUERADE --random-fully
else
  iptables -t nat -A POSTROUTING -o "$WAN" -j MASQUERADE
fi
# Like a real home router, drop unsolicited traffic from the internet. This
# matters for punching: Linux only commits a connection-tracking entry for a
# packet it accepts. Accept a peer's early punch and that entry holds the
# port, so this router's own player's first packet out gets a different
# public port -- not the one the server handed out -- and nothing gets
# through.
iptables -A INPUT -i "$WAN" -m conntrack --ctstate NEW -j DROP
iptables -A FORWARD -i "$WAN" -m conntrack --ctstate NEW -j DROP
echo "router: NAT=${NAT:-cone} out of $WAN"
exec tail -f /dev/null
