# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- One player per name on a server, compared case-insensitively. A HELLO with a
  name someone else holds gets ERROR 8 (name taken); from the same IP address
  it is the same player reconnecting, and the old connection is closed. The C
  client reports the refusal through `psnr_connect_error()`.
- Firewall: `Setup.cmd` offers to add a Windows Firewall rule for the server
  (TCP 36100, one UAC prompt); `setup.sh` prints the ufw/firewalld command when
  a firewall is on. `docs/running.md` covers the server's and players' rules.

- NAT traversal. The server listens on UDP 36100 too. A player's P2P socket
  sends it a PROBE (with a token from HELLO_ACK), and the server records the
  public endpoint the probe came from. Member entries now give each viewer the
  address that works from where they are: that public endpoint from another
  network, the member's LAN address from the same one. Clients built against
  0.2 still work; without a probe nothing changes. The C client builds and
  reads the packets (`psnr_probe_packet`, `psnr_punch_packet`,
  `psnr_probe_reply`, `psnr_is_control`); the caller sends them from its own
  P2P socket. Protocol in `docs/api.md`, "UDP".
- The relay (`-relay`, off by default), for players who can't reach each other
  directly. Streams into a player behind a router go through the server
  (STREAM_CONNECT / STREAM_OFFER / STREAM_ACCEPT / STREAM_READY), and ROUTE
  pushes say which members need it. Datagrams go through the server's UDP
  port (RELAY) when punching doesn't get through, as with a symmetric NAT.
  HELLO_ACK gains a flags byte saying whether the relay is on. The C client
  wraps and unwraps relayed datagrams and opens and accepts relayed streams.
  In the lab, the symmetric-NAT scenario now passes, and every scenario also
  runs a host-to-joiner stream.
- `lab/`: a NAT lab in Docker. A psnr server on a pretend internet, headless
  players behind NATing routers, and three scenarios: two homes (passes), a
  symmetric NAT (fails until psnr has a relay), and two players in one home
  (passes, over LAN addresses). CI runs it.

### Fixed
- `docs/running.md` said the P2P port is UDP only; titles use TCP on it too.

### Changed
- Protocol additions that Simpsons Arcade's Matching2 use needs. Clients
  built against 0.1.0 no longer match.
  - CREATE_ROOM and JOIN_ROOM carry the member's own data, and every member
    entry returns it.
  - SET_ROOM_DATA `which` 2 sets the room's flags, which searchers see.
  - ROOM_MSG pushes say which member the message was sent to.

## [0.1.0] - 2026-09-29

### Added
- Server (`server/`), speaking a big-endian TCP protocol with request ids so
  several asynchronous NP calls can be in flight at once.
- Rooms modelled on sceNpMatching2:
  - create, search, join, leave and kick;
  - external and internal room data, and room messages;
  - owner handoff when the owner leaves;
  - member pushes that carry each peer's address and P2P port.
- Leaderboards modelled on sceNpScore:
  - each player's best score kept, ties to whoever scored first;
  - higher-is-better and lower-is-better boards;
  - ranking by range and by online ID.
- A read-only status page and `/api/stats` JSON on a separate localhost
  listener.
- C client (`client/`) with no threads: `psnr_send` returns a request id, and
  `psnr_poll` hands out replies and pushes without blocking.
- Tests:
  - the Go end-to-end tests run over real sockets;
  - `TestCClient` builds the C client test and runs it against the server.
- Docker image and compose file. `Setup.cmd` / `setup.sh` quick start.
