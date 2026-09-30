# Protocol

Game clients talk to the server over one TCP connection (default port 36100).
The server is in `server/`, the C client is in `client/psnr.h`, and both are
tested against each other (`server/client_test.go`).

## Framing

Every integer is big-endian.

```
u8 type | u16 length | payload (length bytes)
```

- **Requests** (client → server) and **replies** (server → client) start their
  payload with `u32 req`. The client picks the id and the reply echoes it, so
  several requests can be in flight at once. NP's Matching2 and Score APIs are
  asynchronous, and titles do have more than one request outstanding.
- **Pushes** (types `0xA0`–`0xAF`) are server-initiated events. They have no
  request id.

Field notation used below:

- `blob`: a `u16` length followed by that many bytes.
- `[n]`: a fixed-width field, NUL-padded.
- `member`: a room member entry.

`member` is 29 bytes followed by a `blob`:

```
member_id u16 | user_id u32 | online_id [16] | ip [4] | p2p_port u16 | owner u8 | data blob
```

`ip` and `p2p_port` are where the player receiving the entry should send
that member's P2P traffic, so two players can be given different addresses for
the same member:
- **From another network** (a different public address), once the member's
  P2P socket has sent a UDP PROBE (below): the public address and port the
  probe came from, which is its router's mapping.
- **From the same network** (the same public address): the member's LAN
  address from its probe and the port it sent in HELLO. Most routers can't
  loop traffic back in to their own public address.
- **Without a probe:** the address the server sees the member's TCP
  connection come from, and the port it sent in HELLO.

`data` is the member's own data (Matching2 member bin attributes), set when it
creates or joins the room. Game traffic goes between peers directly; the
server never carries it.

## Session

| Type | Name | Request (after `req`) | Reply |
|---|---|---|---|
| 0x01 | HELLO | `comm_id [12]` `online_id [16]` `p2p_port u16` | HELLO_ACK |
| 0x02 | HEARTBEAT | — | none |

**HELLO fields:**
- `comm_id` is the title's NP communication ID (`NPWR00860_00`). It scopes
  rooms and leaderboards: two titles never see each other's.
- `online_id` is the player's name. One player per name on a server,
  compared case-insensitively: a HELLO with a name another connection holds
  gets ERROR 8. From the same IP address it is taken to be the same player
  reconnecting (a title that crashed and restarted), so the server closes the
  old connection and accepts the new one.
- `p2p_port` is the UDP port the title receives peer traffic on, or 0.

HELLO must come first. Any other request before it gets ERROR 7.

**Timeout:** a connection that sends nothing for 90 seconds is dropped. The C
client's `psnr_poll` sends a HEARTBEAT every 20 seconds.

**Replies:**

| Type | Name | Payload (after `req`) |
|---|---|---|
| 0x81 | HELLO_ACK | `user_id u32` `public_ip [4]` `token u32` (the token UDP PROBEs carry; 0.3 on) `flags u8` (bit 0: the server relays; 0.3 on) |
| 0x93 | OK | — |
| 0x8F | ERROR | `code u32` |

**Error codes:**

| Code | Meaning |
|---|---|
| 1 | bad request (malformed, or a value out of range) |
| 2 | not found |
| 3 | room full |
| 4 | not the room owner |
| 5 | not in that room |
| 6 | server room limit reached |
| 7 | no HELLO yet |
| 8 | name taken (HELLO): another player on this server has it |
| 9 | no relay (STREAM_*): the server runs without `-relay` |

## Rooms

These are modelled on sceNpMatching2 rooms.

- A room belongs to the creator's title.
- Member ids count up from 1 in join order.
- The creator is the owner. If the owner leaves, ownership passes to the
  longest-standing member.
- The room closes when the last member leaves or disconnects.

| Type | Name | Request (after `req`) | Reply |
|---|---|---|---|
| 0x10 | CREATE_ROOM | `max u8` `flags u32` `external blob` `internal blob` `member_data blob` | ROOM_JOINED |
| 0x11 | SEARCH_ROOMS | `start u16` `max u16` | ROOM_LIST |
| 0x12 | JOIN_ROOM | `room_id u64` `member_data blob` | ROOM_JOINED |
| 0x13 | LEAVE_ROOM | `room_id u64` | OK |
| 0x14 | SET_ROOM_DATA | `room_id u64` `which u8` `data blob` | OK |
| 0x15 | ROOM_MESSAGE | `room_id u64` `to u16` `data blob` | OK |
| 0x16 | KICK_MEMBER | `room_id u64` `member_id u16` | OK |

**Details:**
- **Room data:** `external` is shown to searchers. `internal` is shown only to
  members.
- **SET_ROOM_DATA:** only the owner may call it. `which` selects the field:
  0 = external, 1 = internal, 2 = flags (`data` is a 4-byte u32). Flags are
  opaque to the server and shown to searchers in ROOM_LIST, so a title can
  mark its room closed.
- **ROOM_MESSAGE:** `to` is a member id, or 0 for every member except the
  sender.
- **KICK_MEMBER:** only the owner may call it.
- **SEARCH_ROOMS:** lists rooms of the caller's title that are not full,
  oldest first. `start` is a 0-based offset into that list.
- **JOIN_ROOM** on a room the caller is already in returns the room again
  instead of an error.

**Replies:**

| Type | Name | Payload (after `req`) |
|---|---|---|
| 0x90 | ROOM_JOINED | `room_id u64` `my_member u16` `owner u16` `max u8` `flags u32` `external blob` `internal blob` `count u8` `member × count` |
| 0x91 | ROOM_LIST | `total u16` `count u16`, then `count` × (`room_id u64` `owner online_id [16]` `cur u8` `max u8` `flags u32` `external blob`) |

**Pushes.** These go to every member except the one who acted, unless noted.

| Type | Name | Payload |
|---|---|---|
| 0xA1 | MEMBER_JOINED | `room_id u64` `member` |
| 0xA2 | MEMBER_LEFT | `room_id u64` `member_id u16` `owner u16` (the owner after the leave) |
| 0xA3 | ROOM_DATA | `room_id u64` `which u8` `data blob` |
| 0xA4 | ROOM_MSG | `room_id u64` `from u16` `to u16` (0 = everyone) `data blob` |
| 0xA5 | KICKED | `room_id u64`, sent to the kicked member only; the rest get MEMBER_LEFT |
| 0xA6 | STREAM_OFFER | `stream_id u32` `from_user u32` `vport u16`: a relayed stream is waiting for you (see "Relay") |
| 0xA7 | ROUTE | `room_id u64` `user u32` `flags u8`: how to reach that member; bit 0 set = streams to it go through the relay. Sent to both sides when a member joins, only with `-relay` |

## Leaderboards

These are modelled on sceNpScore.

- A board is keyed by (title, `board u32`).
- Each player keeps their best score only.
- Ties go to whoever set the score first.

| Type | Name | Request (after `req`) | Reply |
|---|---|---|---|
| 0x20 | RECORD_SCORE | `board u32` `order u8` `score s64` `comment blob` (≤ 64 bytes) | SCORE_RECORDED |
| 0x21 | GET_RANKING | `board u32` `start u32` `count u16` (≤ 100) | RANKING |
| 0x22 | GET_RANKING_BY_ID | `board u32` `n u16` (≤ 100) `online_id [16] × n` | RANKING |

**Details:**
- **`order`:** 0 = higher is better, 1 = lower is better (lap times). The
  first RECORD_SCORE on a board sets it. On PSN this is server-side board
  config that the title never sends, so the client passes the order it knows
  from the title.
- **GET_RANKING `start`:** a 1-based rank.
- **GET_RANKING_BY_ID** returns one row per requested id, in request order.
  An id with no score gets rank 0 and score 0.

**Replies:**

| Type | Name | Payload (after `req`) |
|---|---|---|
| 0xB0 | SCORE_RECORDED | `rank u32`: the player's rank after recording (their best stands if this one is worse) |
| 0xB1 | RANKING | `total u32` `count u16`, then `count` × (`rank u32` `online_id [16]` `score s64` `comment blob` `recorded_unix u64`) |

## UDP

The server also listens on UDP, on the same port as TCP (36100). This is how a
player behind a router gets reachable: the router gives the title's P2P
socket a public port that the server can't see over TCP. Every packet starts
with the four bytes `PSNR` and a type byte.

| Type | Name | From → to | Payload |
|---|---|---|---|
| 0x01 | PROBE | P2P socket → server | `user_id u32` `token u32` `local_ip [4]` |
| 0x81 | PROBE_REPLY | server → P2P socket | `public_ip [4]` `public_port u16` |
| 0x02 | PUNCH | P2P socket → peer | `user_id u32` |

- **PROBE.** Sent from the P2P socket after HELLO, repeated until a
  PROBE_REPLY arrives, and again now and then, because routers forget idle
  mappings. `user_id` and `token` come from HELLO_ACK; a probe with the wrong
  token is ignored, so nobody can move another player's endpoint. `local_ip`
  is the client's own address, given to players on the same network.
- **PUNCH.** When a peer appears, a few of these sent to the address its
  member entry gives open this side's router for the peer's traffic. Both
  sides do it.
- **Receiving.** Anything starting with `PSNR` is a control packet; the
  runtime drops it before the title sees it.

A player whose router picks a new public port for every destination
("symmetric NAT") can't be reached this way. `lab/` reproduces all of this in
Docker.

## Relay

For players who can't reach each other directly. The server does this only
with `-relay`: it costs the host's bandwidth. HELLO_ACK's flags say whether it
is on.

**Streams.** A player behind a router can't take an incoming connection. The
server works out who is in that position: a member whose probe came from a
public address that isn't its own, seen from another network. It tells the
other members with a ROUTE push when that member joins. To open a stream to
such a member:

1. Open a new TCP connection to the server and send, as its first message
   (instead of HELLO), STREAM_CONNECT: `req u32` `user_id u32` `token u32`
   `to_user u32` `vport u16`.
2. The target gets a STREAM_OFFER push and opens its own new connection with
   STREAM_ACCEPT: `req u32` `user_id u32` `token u32` `stream_id u32`.
3. The server answers both connections with STREAM_READY (0x96, `req u32`),
   then copies bytes between them until either side closes. From here on
   there is no framing; the connection is the stream.

Both players must share a room. The target has 10 seconds to accept; after
that, and on any other failure, the connecting side gets an ERROR instead of
STREAM_READY.

**Datagrams.** A client that punched a peer but never heard from it directly
sends through the server's UDP port instead:

| Type | From → to | Payload |
|---|---|---|
| 0x03 RELAY | P2P socket → server | `from_user u32` `token u32` `to_user u32` then the datagram |
| 0x03 RELAY | server → P2P socket | `from_user u32` then the datagram |

The server sends it on from its UDP port to the target's probed endpoint. The
target's router already lets packets from there in, because the target's
probes go there. Receivers unwrap it and hand the payload to the title as if
it came from that member's address.

## HTTP

This is a separate listener, `127.0.0.1:36101` by default (`-http`). It is
read-only and has no authentication.

| Path | Returns |
|---|---|
| `/` | status page: rooms and leaderboards, refreshes every 5 s |
| `/api/stats` | the same data as JSON |
