package main

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// startRelay serves TCP and UDP with the relay on (or off).
func startRelay(t *testing.T, on bool) (*Server, string) {
	s, addr := startTCPUDP(t)
	s.relay = on
	return s, addr
}

type player struct {
	c     *tc
	id    uint32
	token uint32
}

func join(t *testing.T, addr, name string) player {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	c := &tc{t: t, conn: conn}
	id, token := helloToken(t, c, name, 3658)
	return player{c, id, token}
}

// meet puts a and b in one room: a creates it, b joins.
func meet(t *testing.T, a, b player) uint64 {
	w := &writer{}
	w.u8(4)
	w.u32(0)
	w.blob(nil)
	w.blob(nil)
	w.blob(nil)
	rt, r := a.c.call(msgCreateRoom, w.b)
	if rt != msgRoomJoined {
		t.Fatalf("create: 0x%02X", rt)
	}
	room := r.u64()
	w = &writer{}
	w.u64(room)
	w.blob(nil)
	if rt, _ := b.c.call(msgJoinRoom, w.b); rt != msgRoomJoined {
		t.Fatalf("join: 0x%02X", rt)
	}
	return room
}

func udpSock(t *testing.T) *net.UDPConn {
	u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { u.Close() })
	return u
}

// probeFrom registers u as p's P2P socket.
func probeFrom(t *testing.T, u *net.UDPConn, addr string, p player) {
	srv, _ := net.ResolveUDPAddr("udp", addr)
	pkt := append([]byte(udpMagic), udpProbe)
	pkt = binary.BigEndian.AppendUint32(pkt, p.id)
	pkt = binary.BigEndian.AppendUint32(pkt, p.token)
	pkt = append(pkt, 127, 0, 0, 1)
	u.WriteTo(pkt, srv)
	u.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	if _, _, err := u.ReadFrom(buf); err != nil {
		t.Fatal("no probe reply")
	}
}

func relayPkt(from player, token uint32, to uint32, payload string) []byte {
	pkt := append([]byte(udpMagic), udpRelay)
	pkt = binary.BigEndian.AppendUint32(pkt, from.id)
	pkt = binary.BigEndian.AppendUint32(pkt, token)
	pkt = binary.BigEndian.AppendUint32(pkt, to)
	return append(pkt, payload...)
}

// recvWithin returns the next datagram on u, or nil after d.
func recvWithin(u *net.UDPConn, d time.Duration) []byte {
	u.SetReadDeadline(time.Now().Add(d))
	buf := make([]byte, 256)
	n, _, err := u.ReadFrom(buf)
	if err != nil {
		return nil
	}
	return buf[:n]
}

// A relayed datagram reaches its target's probed socket from the server,
// with the sender in front; nothing else gets relayed.
func TestUDPRelay(t *testing.T) {
	_, addr := startRelay(t, true)
	srv, _ := net.ResolveUDPAddr("udp", addr)
	a, b, loner := join(t, addr, "alice"), join(t, addr, "bob"), join(t, addr, "carol")
	meet(t, a, b)
	ua, ub := udpSock(t), udpSock(t)
	probeFrom(t, ub, addr, b)

	ua.WriteTo(relayPkt(a, a.token, b.id, "hi bob"), srv)
	got := recvWithin(ub, time.Second)
	if len(got) != 9+6 || string(got[:4]) != udpMagic || got[4] != udpRelay ||
		binary.BigEndian.Uint32(got[5:]) != a.id || string(got[9:]) != "hi bob" {
		t.Fatalf("relayed %q", got)
	}

	ua.WriteTo(relayPkt(a, a.token+1, b.id, "forged"), srv)
	ua.WriteTo(relayPkt(loner, loner.token, b.id, "not in the room"), srv)
	if got := recvWithin(ub, 300*time.Millisecond); got != nil {
		t.Fatalf("relayed %q", got)
	}
}

func TestUDPRelayOff(t *testing.T) {
	_, addr := startRelay(t, false)
	srv, _ := net.ResolveUDPAddr("udp", addr)
	a, b := join(t, addr, "alice"), join(t, addr, "bob")
	meet(t, a, b)
	ua, ub := udpSock(t), udpSock(t)
	probeFrom(t, ub, addr, b)
	ua.WriteTo(relayPkt(a, a.token, b.id, "hi"), srv)
	if got := recvWithin(ub, 300*time.Millisecond); got != nil {
		t.Fatal("relayed with -relay off")
	}
}

// streamLeg opens a relay connection with its first message.
func streamLeg(t *testing.T, addr string, typ byte, body []byte) (net.Conn, byte) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	w := &writer{}
	w.u32(1)
	w.bytes(body)
	writePacket(conn, typ, w.b)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	rt, _, err := readPacket(conn)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Time{})
	return conn, rt
}

// A stream through the relay: connect, the target is offered it, accepts,
// and bytes flow both ways.
func TestStreamRelay(t *testing.T) {
	_, addr := startRelay(t, true)
	a, b := join(t, addr, "alice"), join(t, addr, "bob")
	meet(t, a, b)

	type leg struct {
		c  net.Conn
		rt byte
	}
	done := make(chan leg, 1)
	go func() {
		w := &writer{}
		w.u32(a.id)
		w.u32(a.token)
		w.u32(b.id)
		w.u16(3658)
		c, rt := streamLeg(t, addr, msgStreamConnect, w.b)
		done <- leg{c, rt}
	}()

	var offer *reader
	for _, p := range b.c.pushes { // bob may already hold pushes from the join
		if p.t == msgStreamOffer {
			offer = &reader{b: p.p}
		}
	}
	for offer == nil {
		pt, pp := b.c.read()
		if pt == msgStreamOffer {
			offer = &reader{b: pp}
		}
	}
	id, from, vport := offer.u32(), offer.u32(), offer.u16()
	if from != a.id || vport != 3658 {
		t.Fatalf("offer from %d vport %d", from, vport)
	}
	w := &writer{}
	w.u32(b.id)
	w.u32(b.token)
	w.u32(id)
	bc, rt := streamLeg(t, addr, msgStreamAccept, w.b)
	if rt != msgStreamReady {
		t.Fatalf("accept: 0x%02X", rt)
	}
	al := <-done
	if al.rt != msgStreamReady {
		t.Fatalf("connect: 0x%02X", al.rt)
	}

	al.c.Write([]byte("setup"))
	buf := make([]byte, 5)
	bc.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(bc, buf); err != nil || string(buf) != "setup" {
		t.Fatalf("bob read %q, %v", buf, err)
	}
	bc.Write([]byte("ack"))
	al.c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(al.c, buf[:3]); err != nil || string(buf[:3]) != "ack" {
		t.Fatalf("alice read %q, %v", buf[:3], err)
	}
}

// Streams need the relay only to a player behind a router, seen from outside
// its network, and only with -relay on.
func TestRelayStreamRule(t *testing.T) {
	s := NewServer(10, false)
	s.relay = true
	natd := &client{ip: [4]byte{203, 0, 113, 7}, udpIP: [4]byte{203, 0, 113, 7}, udpPort: 40001,
		localIP: [4]byte{192, 168, 1, 5}}
	open := &client{ip: [4]byte{198, 51, 100, 9}, udpIP: [4]byte{198, 51, 100, 9}, udpPort: 3658,
		localIP: [4]byte{198, 51, 100, 9}}
	sameHouse := &client{ip: [4]byte{203, 0, 113, 7}}
	if !s.relayStream(natd, open) {
		t.Fatal("a stream into a NAT from outside should use the relay")
	}
	if s.relayStream(open, natd) {
		t.Fatal("a player with a public address takes streams directly")
	}
	if s.relayStream(natd, sameHouse) {
		t.Fatal("the same network connects directly")
	}
	s.relay = false
	if s.relayStream(natd, open) {
		t.Fatal("no relay without -relay")
	}
}

// With -relay on, a join tells both sides how to reach the other.
func TestRoutePushOnJoin(t *testing.T) {
	_, addr := startRelay(t, true)
	a, b := join(t, addr, "alice"), join(t, addr, "bob")
	meet(t, a, b)
	r := b.c.next(msgRoute)
	r.u64()
	if who, flags := r.u32(), r.u8(); who != a.id || flags != 0 {
		t.Fatalf("route about %d flags %d", who, flags)
	}
}
