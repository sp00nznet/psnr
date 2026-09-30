# Roadmap

## Scope

psnr is for a group of players running their own server: a LAN, a tailnet, or
friends connecting to one person's machine. The goal is for that group to have
everything a title's online mode needs: matchmaking, friends, invites,
leaderboards, per-player storage.

It is **not** a public PSN replacement, and won't grow into one:
- no public instance and no server directory;
- no accounts or passwords, just a name each player sets;
- no PSN protocol and no impersonation of Sony's servers. Titles talk to
  psnr through ps3recomp's own NP modules and psnr's own protocol.

## Done

- **Sockets:** real host sockets behind `PS3_NET_ONLINE`
  ([ps3recomp#196](https://github.com/sp00nznet/ps3recomp/pull/196), merged).
- **Rooms and leaderboards:** Matching2, Score, Lookup and Avc2 over psnr
  ([ps3recomp#200](https://github.com/sp00nznet/ps3recomp/pull/200), draft).
- **First online match:** Simpsons Arcade, two instances on one machine, lobby
  to Stage 1 in sync. It also needed a lifter fix in the title's zlib
  ([ps3recomp#202](https://github.com/sp00nznet/ps3recomp/pull/202)).

## 1. Players

- **A name per player.** `--username NAME` on the game's command line, or
  `PS3_NP_ONLINE_ID`. Without either, an online game uses the OS login name.
  Offline stays "PS3Player", so offline saves don't change. The name also
  derives the console identity (OpenPSID, MAC), which titles use to tell
  players apart.
- **`--psnr host[:port]`** to go online with one flag instead of two
  environment variables.
- **One name everywhere:** `sceNpManagerGetOnlineId`, the NP ID and psnr all
  report the same name.
- **Duplicate names:** the server refuses a second player taking a name
  that is already connected.

## 2. Across the internet

Friends in different homes are behind routers, and peers connect to each
other directly.

- **Done:**
  - **Address discovery and hole punching:** UDP probes and per-viewer
    endpoints (#6), with `lab/` reproducing it in Docker.
  - **The relay** (`-relay`): streams into a player behind a router, and
    datagrams when punching fails.
  - **Simpsons across machines:** matches across two machines on a LAN,
    with the host behind a real NAT, and with the joiner behind one (the
    game setup through the relay).
- **Next:**
  - **A guide for the simple case:** forward each player's P2P port, or use a
    tailnet, where every machine can already reach the others.
- **Later:**
  - **UPnP:** open the P2P port on the player's router automatically.
  - **P2P streams over UDP**, as a console does it, if a title needs direct
    low-latency streams.

## 3. Friends and invites (sceNpBasic)

- **Friends list:** by default, everyone on the same server. The group
  running the server already trusts each other, so no friend requests.
- **Presence:** online, in which title, in which room. The server already
  knows all three.
- **Invites and join-a-friend:** a message carrying a room to join, delivered
  through sceNpBasic's message and event APIs.
- **Private rooms:** Matching2 room passwords, private slots, join by room ID.
- **System dialogs.** Many titles open the console's own "invite a friend"
  or "messages" dialog rather than drawing their own. The runtime needs a
  simple overlay for those, or a headless default such as inviting everyone
  online.
- **Player history and block list**, kept by the server per name.
- **First step:** survey which titles call the invite and friend APIs, to pick
  one to test against.

## 4. More titles and services

- **Leaderboard screens:** Simpsons' Scoreboards and Crazy Taxi read Score;
  this needs a live check of what they send and show.
- **sceNpTus** (per-player storage on the server), for Jackbox and Twisted
  Metal.
- **P2P vports:** `SOCK_DGRAM_P2P` maps to plain UDP on one port per
  instance. That is fine for one P2P datagram socket per title; a title with
  two needs vport multiplexing.
- **Voice:** Avc2 connects but carries no audio.
- **Titles:** You Don't Know Jack (Matching2 + Signaling + RUDP), the
  Sonic/Gunstar Genesis hub (the same shell as Simpsons), Rampage World Tour
  (sceNpMatching v1).
- **Lobbies** (Matching2 lobbies, as opposed to rooms), if a title needs them.

## 5. Server

- **State on disk:** leaderboards, TUS data, player history. One file per
  server, and `SECURITY.md` updated to say what is kept.
- **Per-title board config:** sort order and score limits from the title
  rather than the client.
- **Status page:** friends, presence and invites alongside rooms and boards;
  trophies, if a title's trophy data is worth showing.

## Out of scope

- **A public service**, as above.
- **Tickets and title servers.** Some titles check a PSN ticket with their
  publisher's server; those servers are gone and psnr won't stand in for them.
- **Third-party game servers:** Demonware (Guitar Hero III), GameSpy (MUA,
  Saints Row 2), EA, Quazal. Each would be its own project.
- **Store, commerce, DRM.**
