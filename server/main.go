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
	verbose := flag.Bool("v", false, "log every room event")
	flag.Parse()

	s := NewServer(*maxRooms, *verbose)
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
