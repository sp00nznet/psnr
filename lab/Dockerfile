# One image for the lab's routers and players: natcheck, plus the tools a
# router needs to NAT.
FROM alpine:3.20
RUN apk add --no-cache build-base iproute2 iptables
WORKDIR /src
COPY client/ client/
COPY lab/natcheck.c lab/
RUN cc -std=gnu11 -Wall -O2 -o /usr/local/bin/natcheck client/psnr.c lab/natcheck.c
COPY lab/router.sh lab/player.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/router.sh /usr/local/bin/player.sh
