# Security

psnr handles player data: online IDs, and each player's IP address and P2P
port. It hands that data to the other players in the same room, because they
need it to connect to each other. Anyone who joins your room learns your
address, the same as on real PSN P2P.

- **No authentication.** Anyone who can reach TCP 36100 can join, and can claim
  any online ID. Run it for people you'd let onto your network: a LAN, a
  tailnet, or friends.
- **The status page** (`-http`, `127.0.0.1:36101` by default) lists every
  connected player's online ID. Don't expose it publicly.
- **Nothing is stored on disk.**

To report a vulnerability, open a private security advisory on the repository,
or email the maintainer. Please don't use a public issue.
