# Running the server

## Run it

```
cd server
go build -o psnr .        # psnr.exe on Windows
./psnr
```

```
2026/09/29 02:17:35 psnr 0.1.0: clients on [::]:36100
2026/09/29 02:17:35 status page on http://127.0.0.1:36101/
```

With Docker:

```
docker compose up -d
```

## Flags

| Flag | Default | What it does |
|---|---|---|
| `-addr` | `:36100` | TCP address for game clients |
| `-http` | `127.0.0.1:36101` | Status page and `/api/stats`. Set it to `""` to turn it off. |
| `-max-rooms` | `1000` | Room limit across all titles |
| `-v` | off | Log every room event, not just connections |

## Ports

| Port | Proto | Open to |
|---|---|---|
| 36100 | TCP | every player (the only port the server needs reachable) |
| 36101 | TCP | you. It has no auth and lists every player's online ID. Keep it on localhost or a LAN, not the internet. |
| the title's P2P port | UDP | the other players. The game opens this port itself. |

Players on the same LAN or tailnet need nothing forwarded. Across the internet,
each player also has to accept the other players' UDP on their P2P port. The
server passes out addresses and relays nothing.

## State, backup, upgrade

- **State:** everything lives in memory. Restarting the server closes every room
  and empties every leaderboard.
- **Backup:** there is nothing to back up.
- **Upgrade:** stop the server, rebuild it, start it again. Titles reconnect when
  they next go online.
