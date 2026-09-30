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
| each player's P2P port (3658 unless `PS3_NET_P2P_PORT` says otherwise) | UDP and TCP | the other players. The game opens this port itself; titles use both (Simpsons Arcade sends its game setup over TCP). |

Players on the same LAN or tailnet need nothing forwarded. Across the internet,
the server's machine must accept TCP 36100, and each player must accept the
other players' traffic on their P2P port. The server passes out addresses and
relays nothing.

## Firewall

**The server (TCP 36100).** On Windows, `Setup.cmd` offers to add the rule. By
hand, from an admin terminal:

```
netsh advfirewall firewall add rule name=psnr dir=in action=allow protocol=TCP localport=36100 program="C:\path\to\psnr\server\psnr.exe"
```

On Linux, `setup.sh` prints the command if a firewall is on:

```
sudo ufw allow 36100/tcp                                     # ufw
sudo firewall-cmd --permanent --add-port=36100/tcp && sudo firewall-cmd --reload   # firewalld
```

**Each player (the P2P port, UDP and TCP).** On Windows, the first time the
game goes online Windows asks whether to allow it; allow it on the network
type you play on (private, or public too). Or add the rule ahead of time:

```
netsh advfirewall firewall add rule name="psnr game" dir=in action=allow program="C:\path\to\game.exe"
```

On Linux: `sudo ufw allow 3658` (both protocols).

A tailnet needs neither rule for players on it.

## State, backup, upgrade

- **State:** everything lives in memory. Restarting the server closes every room
  and empties every leaderboard.
- **Backup:** there is nothing to back up.
- **Upgrade:** stop the server, rebuild it, start it again. Titles reconnect when
  they next go online.
