# Architecture

```
 recompiled title (host A)                               recompiled title (host B)
 ┌───────────────────────────┐                           ┌───────────────────────────┐
 │ guest code                │                           │ guest code                │
 │   sceNpMatching2 / Score  │                           │   sceNpMatching2 / Score  │
 │ ps3recomp HLE modules     │                           │ ps3recomp HLE modules     │
 │   └─ client/psnr.c ───────┼──── TCP 36100 ───┐   ┌────┼── client/psnr.c           │
 │ sys_net (host sockets) ◄──┼───────────── UDP, peer to peer ──────────► sys_net    │
 └───────────────────────────┘                  │   │    └───────────────────────────┘
                                          ┌─────▼───▼─────┐
                                          │ psnr server   │  rooms, leaderboards
                                          │ server/       │  HTTP status :36101
                                          └───────────────┘
```

## The parts

**`server/`** (Go, standard library only)
- Owns every room and every leaderboard.
- Runs one goroutine per connection, and one mutex over all state.
- Handlers run under the lock and return the pushes they owe other clients.
  Those pushes are written after the lock is released, so a slow peer socket
  never stalls another client's request.
- Keeps state in memory only (see [running.md](running.md)).

**`client/`** (C, one `.c` and one `.h`)
- Written in C because ps3recomp's HLE libraries are C17 and will vendor it.
- **Has no threads.** `psnr_send` returns a request id, and replies and pushes
  come out of `psnr_poll`, which never blocks.
- The design follows how ps3recomp already delivers asynchronous events. For
  example, `cellSysutilCheckCallback` drains a queue on the guest thread that
  calls it. An NP module sends when the title starts a request, and drains
  `psnr_poll` from the title's own poll or callback call, so the title's
  callbacks run on the thread it expects. A background reader thread would
  need a second queue and a lock to get to the same place.

## Why the server doesn't carry game traffic

It follows xlive's model: the relay handles discovery, and the game traffic
itself goes peer to peer.

- Each room member entry carries the peer's address as the server sees it,
  plus the P2P port the peer announced in HELLO.
- The title's signaling layer connects to that address directly over
  `sys_net` sockets.

A relay for game traffic would add a hop of latency to every packet. It would
also need a server large enough to carry all of it.

The cost is NAT. Peers behind NAT that can't reach each other need port
forwarding or a shared VPN; Tailscale works. There is no hole punching.

## Why rooms and leaderboards first

These are the two services that show up across the recompiled library:

- **Rooms:** sceNpMatching2 is the multiplayer path for Simpsons Arcade, You
  Don't Know Jack, and the Sonic/Gunstar Genesis hub.
- **Leaderboards:** sceNpScore is used by about twelve surveyed titles,
  including Crazy Taxi.

Titles whose online play ran on their own servers need a stand-in for that
server, not for PSN:

| Server | Titles |
|---|---|
| Demonware | Guitar Hero III |
| GameSpy | Marvel Ultimate Alliance, Saints Row 2 |
| EA | Orange Box, Trivial Pursuit |
| First-party Medius/DNAS | several |

The protocol is modelled on the SDK's concepts, not its structs:

- Matching2 bin attributes travel as opaque `external`/`internal` blobs.
- Error codes are the server's own. The client maps them to whatever SDK error
  the title expects.
- The server never needs an SDK header, and a change in how one title packs its
  room data doesn't touch it.
