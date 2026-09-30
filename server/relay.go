package main

// The relay, for players who can't reach each other directly. Off unless the
// server runs with -relay: it costs the host's bandwidth.
//
// Streams (TCP). A player behind a router can't take an incoming connection,
// so a peer that wants a stream with one opens a second TCP connection to the
// server that starts with STREAM_CONNECT instead of HELLO. The server offers
// the stream to the target over its own connection (STREAM_OFFER); the
// target opens a connection starting with STREAM_ACCEPT; the server answers
// both with STREAM_READY and from then on copies bytes between them. Which
// peers need this is the server's call (ROUTE pushes, below).
//
// Datagrams (UDP). A RELAY packet to the server's UDP port carries the target
// player; the server sends the payload on from that same port, where the
// target's router already has a mapping (its probes go there), with the
// sender's id in front. Clients switch to it for a peer they punched but
// never heard from directly: a symmetric NAT on either side.

import (
	"encoding/binary"
	"io"
	"net"
	"sync/atomic"
	"time"
)

const (
	msgStreamConnect = 0x30 // first message on a relay stream connection
	msgStreamAccept  = 0x31
	msgStreamReady   = 0x96 // server -> both ends: raw bytes follow
	msgStreamOffer   = 0xA6 // push: stream_id u32 | from_user u32 | vport u16
	msgRoute         = 0xA7 // push: room u64 | user u32 | flags u8

	routeStreamRelay = 1 // flags bit 0: streams to that player go through the relay

	udpRelay = 0x03

	streamAcceptTimeout = 10 * time.Second
)

type stream struct {
	id     uint32
	from   *client
	to     *client
	accept chan net.Conn // the target's leg, once it accepts
}

// relayStream is how a player needing the relay for streams is told.
// The target can't take a direct connection when it's behind a router (its
// probe came from a public address that isn't its own) and the viewer isn't
// on its network. Without a probe, direct is assumed.
func (s *Server) relayStream(target, viewer *client) bool {
	return s.relay && target.udpPort != 0 && target.udpIP != target.localIP && viewer.ip != target.ip
}

func (s *Server) routePush(r *room, about, to *client) push {
	w := &writer{}
	w.u64(r.id)
	w.u32(about.id)
	var flags uint8
	if s.relayStream(about, to) {
		flags |= routeStreamRelay
	}
	w.u8(flags)
	return push{to, msgRoute, w.b}
}

func sharesRoom(a, b *client) bool {
	for id := range a.rooms {
		if b.rooms[id] != nil {
			return true
		}
	}
	return false
}

// streamLeg handles a connection whose first message is STREAM_CONNECT or
// STREAM_ACCEPT. It reports whether it handed the connection to another
// goroutine (which then owns it).
func (s *Server) streamLeg(conn net.Conn, t byte, p []byte) (handedOff bool) {
	r := &reader{b: p}
	req, user, token := r.u32(), r.u32(), r.u32()
	fail := func(code uint32) bool {
		writePacket(conn, msgError, errReply(req, code))
		return false
	}
	if r.err != nil {
		return fail(errBadRequest)
	}
	if !s.relay {
		return fail(errNoRelay)
	}
	ready := func() error {
		w := &writer{}
		w.u32(req)
		return writePacket(conn, msgStreamReady, w.b)
	}

	if t == msgStreamAccept {
		id := r.u32()
		s.mu.Lock()
		st := s.streams[id]
		ok := r.err == nil && st != nil && st.to.id == user && st.to.token == token
		if ok {
			delete(s.streams, id)
		}
		s.mu.Unlock()
		if !ok {
			return fail(errNotFound)
		}
		if ready() != nil {
			return false
		}
		st.accept <- conn
		return true
	}

	toUser, vport := r.u32(), r.u16()
	s.mu.Lock()
	from, to := s.clients[user], s.clients[toUser]
	ok := r.err == nil && from != nil && from.token == token && to != nil && to.commID != "" && sharesRoom(from, to)
	var st *stream
	if ok {
		s.nextStream++
		st = &stream{id: s.nextStream, from: from, to: to, accept: make(chan net.Conn, 1)}
		s.streams[st.id] = st
	}
	s.mu.Unlock()
	if !ok {
		return fail(errNotFound)
	}
	w := &writer{}
	w.u32(st.id)
	w.u32(from.id)
	w.u16(vport)
	s.flush([]push{{to, msgStreamOffer, w.b}})
	s.logf("stream %d: %s -> %s (vport %d) via the relay", st.id, from.onlineID, to.onlineID, vport)

	var other net.Conn
	select {
	case other = <-st.accept:
	case <-time.After(streamAcceptTimeout):
		s.mu.Lock()
		delete(s.streams, st.id)
		s.mu.Unlock()
		return fail(errNotFound)
	}
	if ready() != nil {
		other.Close()
		return false
	}
	conn.SetDeadline(time.Time{})
	other.SetDeadline(time.Time{})
	go s.splice(other, conn)
	s.splice(conn, other)
	return false
}

// splice copies src to dst until either end closes, then closes both.
func (s *Server) splice(dst, src net.Conn) {
	n, _ := io.Copy(dst, src)
	atomic.AddUint64(&s.relayed, uint64(n))
	dst.Close()
	src.Close()
}

// relayUDP forwards a RELAY packet: "PSNR" 03 | from u32 | token u32 | to u32 |
// payload becomes "PSNR" 03 | from u32 | payload, sent to the target's probed
// endpoint from the server's UDP port.
func (s *Server) relayUDP(pc net.PacketConn, pkt []byte) {
	if !s.relay || len(pkt) < 17 {
		return
	}
	from, token, toID := binary.BigEndian.Uint32(pkt[5:]), binary.BigEndian.Uint32(pkt[9:]), binary.BigEndian.Uint32(pkt[13:])
	s.mu.Lock()
	src, dst := s.clients[from], s.clients[toID]
	ok := src != nil && src.token == token && dst != nil && dst.udpPort != 0 && sharesRoom(src, dst)
	var to *net.UDPAddr
	if ok {
		to = &net.UDPAddr{IP: net.IP(append([]byte(nil), dst.udpIP[:]...)), Port: int(dst.udpPort)}
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	out := make([]byte, 0, len(pkt)-8)
	out = append(out, udpMagic...)
	out = append(out, udpRelay)
	out = binary.BigEndian.AppendUint32(out, from)
	out = append(out, pkt[17:]...)
	pc.WriteTo(out, to)
	atomic.AddUint64(&s.relayed, uint64(len(pkt)-17))
}
