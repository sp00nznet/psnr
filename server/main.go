// psnr: a stand-in for PSN's matchmaking and leaderboard services, for
// statically recompiled PS3 titles. The runtime's sceNp modules talk to it
// over the protocol in docs/api.md; peers then talk to each other directly.
// See docs/architecture.md.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
)

var version = "0.1.0"

func main() {
	addr := flag.String("addr", ":36100", "TCP and UDP address for game clients")
	httpAddr := flag.String("http", "127.0.0.1:36101", "HTTP status page and /api/stats (empty to disable)")
	maxRooms := flag.Int("max-rooms", 1000, "room limit across all titles")
	playerRooms := flag.Int("max-player-rooms", 8, "rooms one player may be in at once (0 for no limit)")
	perAddr := flag.Int("max-conns-per-addr", 32, "open connections per remote address (0 for no limit)")
	reqRate := flag.Int("max-req-rate", 200, "requests per second per connection before it is closed (0 for no limit)")
	verbose := flag.Bool("v", false, "log every room event")
	relay := flag.Bool("relay", false, "carry traffic between players who can't reach each other directly (uses this server's bandwidth)")
	flag.Parse()

	s := NewServer(*maxRooms, *verbose)
	s.relay = *relay
	s.maxPlayerRooms, s.maxPerAddr, s.maxReqRate = *playerRooms, *perAddr, *reqRate
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	pc, err := net.ListenPacket("udp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	go func() { log.Fatal(s.ServeUDP(pc)) }()
	log.Printf("psnr %s: clients on %s (TCP and UDP)", version, ln.Addr())
	if *httpAddr != "" {
		log.Printf("status page on http://%s/", *httpAddr)
		go func() { log.Fatal(http.ListenAndServe(*httpAddr, s.HTTP())) }()
	}
	log.Fatal(s.Serve(ln))
}
