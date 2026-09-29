# Roadmap

## Next: games talking to it

The work is in ps3recomp. Each item is its own PR there, and each depends on
the one before it.

1. **Guest sockets:** done in [ps3recomp#196](https://github.com/sp00nznet/ps3recomp/pull/196)
   (merged). Real host sockets behind `PS3_NET_ONLINE`.
2. **Client, Matching2, Score, Lookup, Avc2:** in
   [ps3recomp#200](https://github.com/sp00nznet/ps3recomp/pull/200) (draft).
   In Simpsons Arcade, two players meet, pick characters and play Stage 1
   together. The game setup transfer also needed a lifter fix in the title's
   zlib, [ps3recomp#202](https://github.com/sp00nznet/ps3recomp/pull/202).
   What's left there:
   - Split `sceNpMatching2.c` to the toolkit's file-size guideline.
   - Play a match through to the end, and across two machines.
3. **P2P vports:** `SOCK_DGRAM_P2P` maps to plain UDP on one port per
   instance. That is fine for one P2P datagram socket per title; a title with
   two needs vport multiplexing.
4. **Leaderboard screens:** Simpsons' Scoreboards and Crazy Taxi both read
   Score. This needs a live check of what they send and show.
5. **Voice:** Avc2 currently carries no audio.
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
