package main

// One goroutine per client connection, one mutex over all room and score
// state. Handlers run under the lock and return the pushes they owe other
// clients; those are written after the lock drops, so a slow peer socket
// never stalls anyone else's request.

import (
	crand "crypto/rand"
	"encoding/binary"
	"io"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const clientTimeout = 90 * time.Second // HELLO/HEARTBEAT keep a client alive

type Server struct {
	verbose bool
	relay   bool // -relay: carry traffic for players who can't reach each other (relay.go)

	// ponytail: one lock for everything; per-title locks if one server ever
	// carries enough titles for it to show.
	mu       sync.Mutex
	clients  map[uint32]*client
	rooms    map[uint64]*room
	boards   map[boardKey]*board
	nextID   uint32
	nextRoom uint64
	maxRooms int
	started  time.Time
	conns    uint64

	streams    map[uint32]*stream // relay streams waiting for their target to accept
	nextStream uint32
	relayed    uint64 // bytes carried by the relay (atomic)
}

type client struct {
	id       uint32
	conn     net.Conn
	commID   string // NP communication ID, e.g. NPWR00860_00: the title
	onlineID string
	ip       [4]byte // as the server sees it
	p2pPort  uint16  // UDP port the title receives peer traffic on
	token    uint32  // from HELLO_ACK; UDP probes must carry it
	udpIP    [4]byte // P2P socket's public endpoint, from its UDP probe
	udpPort  uint16
	localIP  [4]byte // the client's own address, from its UDP probe
	rooms    map[uint64]*room
	sendMu   sync.Mutex
}

type push struct {
	to *client
	t  byte
	p  []byte
}

func NewServer(maxRooms int, verbose bool) *Server {
	return &Server{
		verbose:  verbose,
		clients:  map[uint32]*client{},
		rooms:    map[uint64]*room{},
		boards:   map[boardKey]*board{},
		streams:  map[uint32]*stream{},
		maxRooms: maxRooms,
		started:  time.Now(),
	}
}

func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.nextID++
		s.conns++
		c := &client{id: s.nextID, conn: conn, rooms: map[uint64]*room{}}
		if a, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
			copy(c.ip[:], a.IP.To4())
		}
		s.clients[c.id] = c
		s.mu.Unlock()
		go s.run(c)
	}
}

func (s *Server) run(c *client) {
	relayLeg := false // a relay stream connection, not a player
	defer func() {
		if relayLeg {
			return
		}
		c.conn.Close()
		s.mu.Lock()
		var out []push
		for _, r := range c.rooms {
			out = append(out, s.leave(r, c)...)
		}
		delete(s.clients, c.id)
		s.mu.Unlock()
		s.flush(out)
		s.logf("client %d (%s) gone", c.id, c.onlineID)
	}()

	for first := true; ; first = false {
		c.conn.SetReadDeadline(time.Now().Add(clientTimeout))
		t, p, err := readPacket(c.conn)
		if err != nil {
			if err != io.EOF {
				s.logf("client %d read: %v", c.id, err)
			}
			return
		}
		if first && (t == msgStreamConnect || t == msgStreamAccept) {
			relayLeg = true
			s.mu.Lock()
			delete(s.clients, c.id)
			s.mu.Unlock()
			if !s.streamLeg(c.conn, t, p) {
				c.conn.Close()
			}
			return
		}
		s.handle(c, t, p)
	}
}

func (s *Server) handle(c *client, t byte, p []byte) {
	r := &reader{b: p}
	req := r.u32()
	if r.err != nil {
		return
	}
	if t != msgHello && c.commID == "" {
		c.send(msgError, errReply(req, errNoHello))
		return
	}

	s.mu.Lock()
	var reply []byte
	var rt byte
	var out []push
	switch t {
	case msgHello:
		rt, reply = s.hello(c, req, r)
	case msgHeartbeat:
		s.mu.Unlock()
		return
	case msgCreateRoom:
		rt, reply = s.createRoom(c, req, r)
	case msgSearchRooms:
		rt, reply = s.searchRooms(c, req, r)
	case msgJoinRoom:
		rt, reply, out = s.joinRoom(c, req, r)
	case msgLeaveRoom:
		rt, reply, out = s.leaveRoom(c, req, r)
	case msgSetRoomData:
		rt, reply, out = s.setRoomData(c, req, r)
	case msgRoomMessage:
		rt, reply, out = s.roomMessage(c, req, r)
	case msgKickMember:
		rt, reply, out = s.kick(c, req, r)
	case msgRecordScore:
		rt, reply = s.recordScore(c, req, r)
	case msgGetRanking:
		rt, reply = s.getRanking(c, req, r)
	case msgGetRankingID:
		rt, reply = s.getRankingByID(c, req, r)
	default:
		rt, reply = msgError, errReply(req, errBadRequest)
	}
	s.mu.Unlock()

	c.send(rt, reply)
	s.flush(out)
}

func (s *Server) hello(c *client, req uint32, r *reader) (byte, []byte) {
	commID, onlineID, port := r.str(12), r.str(16), r.u16()
	if r.err != nil || commID == "" {
		return msgError, errReply(req, errBadRequest)
	}
	// One player per name, as PSN IDs are unique, compared case-insensitively
	// like them. The same name from the same address is the same player
	// reconnecting -- a title that crashed and restarted -- whose old
	// connection can linger until clientTimeout, so it gives way.
	for _, o := range s.clients {
		if o == c || o.commID == "" || !strings.EqualFold(o.onlineID, onlineID) {
			continue
		}
		if o.ip != c.ip {
			return msgError, errReply(req, errNameTaken)
		}
		log.Printf("client %d: %q reconnected as client %d", o.id, onlineID, c.id)
		o.commID = "" // no longer counts; its run loop cleans up once Close lands
		o.conn.Close()
	}
	c.commID, c.onlineID, c.p2pPort = commID, onlineID, port
	var tok [4]byte
	crand.Read(tok[:])
	c.token = binary.BigEndian.Uint32(tok[:]) | 1 // never 0
	log.Printf("client %d: %s as %q from %d.%d.%d.%d, p2p port %d",
		c.id, commID, onlineID, c.ip[0], c.ip[1], c.ip[2], c.ip[3], port)
	w := &writer{}
	w.u32(req)
	w.u32(c.id)
	w.bytes(c.ip[:])
	w.u32(c.token)
	var flags uint8
	if s.relay {
		flags |= 1 // the relay is available
	}
	w.u8(flags)
	return msgHelloAck, w.b
}

func (s *Server) flush(out []push) {
	for _, m := range out {
		m.to.send(m.t, m.p)
	}
}

func (c *client) send(t byte, p []byte) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if err := writePacket(c.conn, t, p); err != nil {
		c.conn.Close() // the reader goroutine sees it and cleans up
	}
}

func errReply(req uint32, code uint32) []byte {
	w := &writer{}
	w.u32(req)
	w.u32(code)
	return w.b
}

func okReply(req uint32) []byte {
	w := &writer{}
	w.u32(req)
	return w.b
}

func (s *Server) logf(f string, a ...any) {
	if s.verbose {
		log.Printf(f, a...)
	}
}

// sortedRooms returns a title's rooms oldest first, so search pages are stable.
func (s *Server) sortedRooms(commID string) []*room {
	var out []*room
	for _, r := range s.rooms {
		if r.commID == commID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}
