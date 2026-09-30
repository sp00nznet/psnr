# NAT lab

Players in different homes sit behind routers that translate addresses (NAT).
This lab reproduces that in Docker, on one machine: a psnr server on a pretend
internet, and headless players on home networks behind NATing routers. It
tests the part of psnr that gets players through those routers, with the real
C client and real UDP sockets.

```
                 internet 10.99.0.0/24 (no way out)
     ┌───────────────┬─────────────┴─────────────┐
 router_a .2     server .10                   router_b .3
     │ NAT                                        │ NAT
 home_a 192.168.10.0/24                     home_b 192.168.20.0/24
  alice .10 (hosts)  carol .11 (joins)        bob .10 (joins)
```

Run it:

```
lab/run.sh                 # all scenarios
lab/run.sh cone samelan    # some
```

It needs Docker with compose (Docker Desktop on Windows and macOS). The exit
status is non-zero if any scenario doesn't do what it's expected to.

## What a player does

`natcheck` does what the runtime does for a title, minus the title:

1. HELLO over TCP, which returns a token.
2. From its P2P socket, a UDP PROBE to the server carrying that token. The
   server answers with the public address and port it saw, and from then on
   hands that endpoint to players on other networks. Players on the same
   network get the LAN address instead.
3. Host or join a room, which gives it the other player's endpoint.
4. PUNCH packets and PINGs to that endpoint until a PONG comes back.

## Scenarios

| Scenario | Setup | Expected |
|---|---|---|
| `cone` | alice and bob in different homes, ordinary routers | pass |
| `symmetric` | bob's router picks a new public port for every destination | **fail**: the endpoint the server saw is useless to alice. A relay is what fixes this; the scenario stays until psnr has one. |
| `samelan` | alice and carol in the same home | pass, over their LAN addresses |

## What the routers model

- **Masquerading**, as home routers do. Linux keeps a socket's source port
  for every destination, so a peer can reach the mapping the server saw,
  once this side has sent to that peer. `NAT=symmetric` adds
  `--random-fully`: a random port per destination.
- **Dropping unsolicited traffic from the internet**, also as home routers
  do. This turned out to matter. Linux only records a connection for a packet
  it accepts, and an accepted early punch from the peer holds the port. The
  router's own player's first packet out then gets a different public port,
  not the one the server handed out, and nothing gets through.
- **No real internet.** The internet network is internal. The home networks
  can't be, because Docker drops a packet addressed outside an internal
  network's subnet even on its way to the router. So players route only
  through their router, and routers delete their Docker default route.
