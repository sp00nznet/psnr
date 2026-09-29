package main

import (
	"net"
	"testing"
	"time"
)

// tc is a test client speaking the wire protocol over a real TCP socket.
type tc struct {
	t      *testing.T
	conn   net.Conn
	req    uint32
	pushes []pkt
}

type pkt struct {
	t byte
	p []byte
}

func start(t *testing.T) string {
	s := NewServer(10, false)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go s.Serve(ln)
	return ln.Addr().String()
}

func dial(t *testing.T, addr, commID, onlineID string, port uint16) *tc {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	c := &tc{t: t, conn: conn}
	w := &writer{}
	w.str(commID, 12)
	w.str(onlineID, 16)
	w.u16(port)
	if rt, _ := c.call(msgHello, w.b); rt != msgHelloAck {
		t.Fatalf("hello: got 0x%02X", rt)
	}
	return c
}

// call sends a request and returns its reply (payload after the request
// id), stashing any pushes that arrive first.
func (c *tc) call(t byte, body []byte) (byte, *reader) {
	c.req++
	w := &writer{}
	w.u32(c.req)
	w.bytes(body)
	if err := writePacket(c.conn, t, w.b); err != nil {
		c.t.Fatal(err)
	}
	for {
		rt, p := c.read()
		if rt >= 0xA0 && rt < 0xB0 {
			c.pushes = append(c.pushes, pkt{rt, p})
			continue
		}
		r := &reader{b: p}
		if id := r.u32(); id != c.req {
			c.t.Fatalf("reply for request %d, want %d", id, c.req)
		}
		return rt, r
	}
}

func (c *tc) read() (byte, []byte) {
	c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	t, p, err := readPacket(c.conn)
	if err != nil {
		c.t.Fatal(err)
	}
	return t, p
}

// next returns the next push, from the stash or the wire.
func (c *tc) next(want byte) *reader {
	var p pkt
	if len(c.pushes) > 0 {
		p, c.pushes = c.pushes[0], c.pushes[1:]
	} else {
		p.t, p.p = c.read()
	}
	if p.t != want {
		c.t.Fatalf("push 0x%02X, want 0x%02X", p.t, want)
	}
	return &reader{b: p.p}
}

func TestRooms(t *testing.T) {
	addr := start(t)
	a := dial(t, addr, "NPWR00001_00", "alice", 3658)
	b := dial(t, addr, "NPWR00001_00", "bob", 3659)
	other := dial(t, addr, "NPWR99999_00", "carol", 3658)

	// alice hosts a two-player room
	w := &writer{}
	w.u8(2)
	w.u32(0)
	w.blob([]byte("stage1"))
	w.blob([]byte("secret"))
	w.blob([]byte("homer"))
	rt, r := a.call(msgCreateRoom, w.b)
	if rt != msgRoomJoined {
		t.Fatalf("create: 0x%02X", rt)
	}
	roomID, aMember := r.u64(), r.u16()

	// bob finds it with its external data; another title does not
	w = &writer{}
	w.u16(0)
	w.u16(10)
	_, r = b.call(msgSearchRooms, w.b)
	if total, count := r.u16(), r.u16(); total != 1 || count != 1 {
		t.Fatalf("search: total %d count %d", total, count)
	}
	if id, owner := r.u64(), r.str(16); id != roomID || owner != "alice" {
		t.Fatalf("search: room %d owner %q", id, owner)
	}
	r.u8()
	r.u8()
	r.u32()
	if ext := string(r.blob()); ext != "stage1" {
		t.Fatalf("search: external %q", ext)
	}
	if _, r = other.call(msgSearchRooms, w.b); r.u16() != 0 {
		t.Fatal("room leaked across titles")
	}

	// bob joins: sees both members and the internal data; alice is told,
	// with bob's p2p port and member data
	w = &writer{}
	w.u64(roomID)
	w.blob([]byte("bart"))
	rt, r = b.call(msgJoinRoom, w.b)
	if rt != msgRoomJoined {
		t.Fatalf("join: 0x%02X", rt)
	}
	r.u64()
	bMember, owner := r.u16(), r.u16()
	r.u8()
	r.u32()
	r.blob()
	if in := string(r.blob()); in != "secret" || owner != aMember || r.u8() != 2 {
		t.Fatalf("join: internal %q owner %d", in, owner)
	}
	r.take(29) // alice's entry, up to her data
	if d := string(r.blob()); d != "homer" {
		t.Fatalf("join: alice's member data %q", d)
	}
	p := a.next(msgMemberJoined)
	p.u64()
	if id, _, name := p.u16(), p.u32(), p.str(16); id != bMember || name != "bob" {
		t.Fatalf("member joined: %d %q", id, name)
	}
	p.take(4)
	if port := p.u16(); port != 3659 {
		t.Fatalf("member joined: port %d", port)
	}
	if p.u8(); string(p.blob()) != "bart" {
		t.Fatal("member joined: member data")
	}

	// full now
	c := dial(t, addr, "NPWR00001_00", "dave", 3660)
	if rt, r = c.call(msgJoinRoom, w.b); rt != msgError || r.u32() != errRoomFull {
		t.Fatal("joined a full room")
	}

	// room message alice -> everyone
	w = &writer{}
	w.u64(roomID)
	w.u16(0)
	w.blob([]byte("go"))
	if rt, _ = a.call(msgRoomMessage, w.b); rt != msgOK {
		t.Fatalf("message: 0x%02X", rt)
	}
	p = b.next(msgRoomMsg)
	if p.u64(); p.u16() != aMember || p.u16() != 0 || string(p.blob()) != "go" {
		t.Fatal("room message")
	}

	// the owner closes the room: bob hears, and a searcher sees the flags
	w = &writer{}
	w.u64(roomID)
	w.u8(2)
	w.blob([]byte{0x40, 0, 0, 0})
	if rt, _ = a.call(msgSetRoomData, w.b); rt != msgOK {
		t.Fatalf("set flags: 0x%02X", rt)
	}
	if p = b.next(msgRoomData); p.u64() != roomID || p.u8() != 2 {
		t.Fatal("flags push")
	}

	// only the owner writes room data
	w = &writer{}
	w.u64(roomID)
	w.u8(0)
	w.blob([]byte("x"))
	if rt, r = b.call(msgSetRoomData, w.b); rt != msgError || r.u32() != errNotOwner {
		t.Fatal("non-owner set room data")
	}

	// alice leaves: bob is told and owns the room
	w = &writer{}
	w.u64(roomID)
	if rt, _ = a.call(msgLeaveRoom, w.b); rt != msgOK {
		t.Fatal("leave")
	}
	p = b.next(msgMemberLeft)
	if p.u64(); p.u16() != aMember || p.u16() != bMember {
		t.Fatal("member left / owner handoff")
	}

	// bob disconnects: the room closes
	b.conn.Close()
	time.Sleep(100 * time.Millisecond)
	w = &writer{}
	w.u16(0)
	w.u16(10)
	if _, r = a.call(msgSearchRooms, w.b); r.u16() != 0 {
		t.Fatal("room outlived its last member")
	}
}

func TestScores(t *testing.T) {
	addr := start(t)
	a := dial(t, addr, "NPWR00002_00", "alice", 0)
	b := dial(t, addr, "NPWR00002_00", "bob", 0)

	record := func(c *tc, board uint32, order uint8, score int64) uint32 {
		w := &writer{}
		w.u32(board)
		w.u8(order)
		w.u64(uint64(score))
		w.blob([]byte("gg"))
		rt, r := c.call(msgRecordScore, w.b)
		if rt != msgScoreRecorded {
			t.Fatalf("record: 0x%02X", rt)
		}
		return r.u32()
	}
	if rank := record(a, 1, 0, 100); rank != 1 {
		t.Fatalf("alice rank %d", rank)
	}
	if rank := record(b, 1, 0, 200); rank != 1 {
		t.Fatalf("bob rank %d", rank)
	}
	if rank := record(a, 1, 0, 50); rank != 2 { // worse: her 100 stands
		t.Fatalf("alice rank after worse score %d", rank)
	}

	w := &writer{}
	w.u32(1)
	w.u32(1)
	w.u16(10)
	_, r := a.call(msgGetRanking, w.b)
	if total, n := r.u32(), r.u16(); total != 2 || n != 2 {
		t.Fatalf("ranking: total %d n %d", total, n)
	}
	for _, want := range []struct {
		name  string
		score int64
	}{{"bob", 200}, {"alice", 100}} {
		r.u32()
		if name, score := r.str(16), int64(r.u64()); name != want.name || score != want.score {
			t.Fatalf("ranking: %s %d, want %s %d", name, score, want.name, want.score)
		}
		r.blob()
		r.u64()
	}

	// lower-is-better board: lap times
	record(a, 2, 1, 61000)
	if rank := record(b, 2, 1, 59000); rank != 1 {
		t.Fatalf("ascending board: bob rank %d", rank)
	}

	// friends' ranks, including one with no score
	w = &writer{}
	w.u32(1)
	w.u16(2)
	w.str("alice", 16)
	w.str("nobody", 16)
	_, r = b.call(msgGetRankingID, w.b)
	r.u32()
	r.u16()
	if rank := r.u32(); rank != 2 {
		t.Fatalf("by id: alice rank %d", rank)
	}
	r.str(16)
	r.u64()
	r.blob()
	r.u64()
	if rank, name := r.u32(), r.str(16); rank != 0 || name != "nobody" {
		t.Fatalf("by id: nobody rank %d %q", rank, name)
	}
}

func TestRequestBeforeHello(t *testing.T) {
	conn, err := net.Dial("tcp", start(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := &tc{t: t, conn: conn}
	if rt, r := c.call(msgSearchRooms, []byte{0, 0, 0, 10}); rt != msgError || r.u32() != errNoHello {
		t.Fatal("request before HELLO was served")
	}
}
