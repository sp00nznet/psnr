# Roadmap

## Next: games talking to it

The work is in ps3recomp. Each item is its own PR there, and each depends on
the one before it.

1. **Guest sockets:** [ps3recomp#196](https://github.com/sp00nznet/ps3recomp/pull/196).
   Real host sockets behind `PS3_NET_ONLINE`. Every item below needs it.
2. **Vendor `client/` into ps3recomp** (`libs/network/psnr/`). Server address
   from `PSNR_SERVER=host:port`, online ID from the existing fake-username
   setting.
3. **sceNpScore over psnr**, for Crazy Taxi and about eleven other titles:
   - title and transaction contexts, `RecordScore` and `GetRankingByRange`/`ByNpId`
     (sync and async), `PollAsync`;
   - the missing `sceNp2Init`/`Term`.
4. **sceNpMatching2 over psnr**, for Simpsons Arcade first:
   - context start with its "context started" event;
   - `SearchRoom`, `CreateJoinRoom`, `JoinRoom`, `LeaveRoom`;
   - room data, room messages, `GetEventData`;
   - room and signaling callbacks drained on the guest thread.
   The runtime's Matching2 also needs the SDK's actual export names (`Init2`,
   `ContextStartAsync`, ...).
5. **sceNpSignaling / Matching2 signaling:** report peers connected once a
   member's address is known, with `GetConnectionStatus` and `GetPingInfo`.
   P2P sockets (`SOCK_DGRAM_P2P`) need vport multiplexing over one UDP port.
6. **Second and third titles:** You Don't Know Jack (Matching2 + Signaling +
   RUDP) and the Sonic/Gunstar Genesis hub (the same shell as Simpsons).

## Later

- **Persistent leaderboards:** a snapshot to disk on change.
- **Per-title board config:** sort order, and score limits taken from the
  title rather than passed by the client.
- **Lobbies** (Matching2 lobbies, as opposed to rooms), if a title needs them.
- **sceNpTus** (title user storage), for Jackbox and Twisted Metal.
- **sceNpMatching v1**, for Rampage World Tour.
- **Friends and presence (sceNpBasic):** friend lists from players seen in the
  same rooms, and invites.

## Out of scope

- **Carrying game traffic.** Peers connect directly. A relay for players who
  can't reach each other would be a separate service.
- **Accounts, passwords, tickets.** The online ID is whatever the player sets.
  Nothing here authenticates anyone.
- **Third-party game servers:** Demonware (Guitar Hero III), GameSpy (MUA,
  Saints Row 2), EA, Quazal. Those titles need a stand-in for that company's
  server, not for PSN, and each would be its own project.
- **Store, commerce, DRM.**
