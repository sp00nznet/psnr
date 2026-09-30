#!/bin/sh
# A player behind a router: route everything through it, then run natcheck.
set -e
ip route replace default via "$ROUTER"
exec natcheck "$@"
