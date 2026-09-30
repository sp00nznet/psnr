#!/bin/sh
# The NAT lab: lab/run.sh [cone|symmetric|samelan ...]   (default: all three)
#
#   cone       alice and bob in two homes behind ordinary routers. The server
#              hands each the other's public endpoint, they punch, and ping
#              gets through. Expected to pass.
#   symmetric  bob's router picks a new port per destination, so the endpoint
#              the server saw is useless to alice. Expected to FAIL until
#              psnr has a relay; kept to show what the relay is for.
#   samelan    alice and carol in the same home. The server gives carol
#              alice's LAN address, not their shared public one. Expected to
#              pass.
#
# Needs Docker with compose. Exit status is non-zero if any scenario does
# something other than what it's expected to.
cd "$(dirname "$0")" || exit 1
compose() { docker compose --profile samelan "$@"; }

docker build -q -t psnr-lab-server .. >/dev/null &&
  docker build -q -t psnr-lab-node -f Dockerfile .. >/dev/null || { echo "image build failed"; exit 1; }

bad=0
scenario() {   # name expect "players" [VAR=value ...]
  name=$1 expect=$2 players=$3
  shift 3
  echo "== $name (expect $expect)"
  compose down -v --remove-orphans >/dev/null 2>&1
  if ! env "$@" docker compose --profile samelan up -d server router_a router_b $players \
       > /tmp/psnr-lab-up.log 2>&1; then
    cat /tmp/psnr-lab-up.log
    bad=1
    return
  fi
  got=pass
  for p in $players; do
    [ "$(docker wait "psnr-lab-$p-1")" = 0 ] || got=fail
  done
  compose logs --no-log-prefix $players | grep -E "seen at|other player|PASS|FAIL"
  if [ "$name" = samelan ] && ! compose logs carol | grep -q "other player is at 192.168.10.10:"; then
    echo "carol wasn't given alice's LAN address"
    got=fail
  fi
  if [ "$got" = "$expect" ]; then echo "-> $got, as expected"; else echo "-> $got, expected $expect"; bad=1; fi
}

for s in ${@:-cone symmetric samelan}; do
  case $s in
    cone)      scenario cone pass "alice bob" ;;
    symmetric) scenario symmetric fail "alice bob" NAT_B=symmetric ;;
    samelan)   scenario samelan pass "alice carol" ;;
    *)         echo "unknown scenario $s"; bad=1 ;;
  esac
done
compose down -v --remove-orphans >/dev/null 2>&1
exit $bad
