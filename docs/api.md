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

The `ip` field is the address the server sees the member connecting from. The
`p2p_port` is the port that member sent in HELLO. `data` is the member's own
data (Matching2 member bin attributes), set when it creates or joins the room. Together they give peers each
other's addresses. Game traffic goes between peers directly; the server never
carries it.

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
| 0x81 | HELLO_ACK | `user_id u32` `public_ip [4]` |
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

## HTTP

This is a separate listener, `127.0.0.1:36101` by default (`-http`). It is
read-only and has no authentication.

| Path | Returns |
|---|---|
| `/` | status page: rooms and leaderboards, refreshes every 5 s |
| `/api/stats` | the same data as JSON |
