package main

import (
	"bytes"
	"io"
	"net"
	"testing"
)

// FuzzReadPacket feeds arbitrary bytes to the frame parser: it must return a
// frame or an error, never panic, and never return more than a frame holds.
func FuzzReadPacket(f *testing.F) {
	f.Add([]byte{msgHeartbeat, 0, 4, 0, 0, 0, 1})
	f.Add([]byte{msgHello, 0xFF, 0xFF})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		_, p, err := readPacket(bytes.NewReader(b))
		if err == nil && len(p) > maxPayload {
			t.Fatalf("payload of %d bytes", len(p))
		}
	})
}

// FuzzHandle sends one arbitrary request, of any type, to a server with a
// player in a room. Whatever the payload, the handler must not panic.
func FuzzHandle(f *testing.F) {
	body := func(w *writer) []byte { return append([]byte{0, 0, 0, 1}, w.b...) }
	create := &writer{}
	create.u8(2)
	create.u32(0)
	create.blob([]byte("x"))
	create.blob(nil)
	create.blob(nil)
	join := &writer{}
	join.u64(1)
	join.blob(nil)
	score := &writer{}
	score.u32(1)
	score.u8(0)
	score.u64(100)
	score.blob([]byte("c"))
	f.Add(byte(msgCreateRoom), body(create))
	f.Add(byte(msgJoinRoom), body(join))
	f.Add(byte(msgRecordScore), body(score))
	f.Add(byte(msgGetRankingID), body(&writer{}))
	f.Add(byte(msgSetRoomData), body(&writer{}))
	f.Add(byte(0xEE), []byte{0, 0, 0, 1})

	f.Fuzz(func(t *testing.T, typ byte, payload []byte) {
		s := NewServer(10, false)
		a, peer := net.Pipe()
		defer a.Close()
		go io.Copy(io.Discard, peer) // the server's replies and pushes
		defer peer.Close()

		owner := &client{id: 1, conn: a, commID: "NPWR00001_00", onlineID: "alice", rooms: map[uint64]*room{}}
		s.clients[owner.id] = owner
		s.handle(owner, msgCreateRoom, body(create))
		if typ == msgHello || typ == msgStreamConnect || typ == msgStreamAccept {
			return // HELLO and relay legs are exercised by the connection tests
		}
		s.handle(owner, typ, payload)
	})
}
