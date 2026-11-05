package main

import (
	"net"
	"testing"
	"time"
)

func createBody(ext []byte) []byte {
	w := &writer{}
	w.u8(2)
	w.u32(0)
	w.blob(ext)
	w.blob(nil)
	w.blob(nil)
	return w.b
}

func TestBlobLimit(t *testing.T) {
	a := dial(t, start(t), "NPWR00001_00", "alice", 3658)
	if rt, _ := a.call(msgCreateRoom, createBody(make([]byte, maxRoomBlob+1))); rt != msgError {
		t.Fatalf("oversize blob: got 0x%02X, want error", rt)
	}
	if rt, _ := a.call(msgCreateRoom, createBody(make([]byte, maxRoomBlob))); rt != msgRoomJoined {
		t.Fatalf("blob at the limit: got 0x%02X, want ROOM_JOINED", rt)
	}
}

func TestPlayerRoomLimit(t *testing.T) {
	s := NewServer(100, false)
	s.maxPlayerRooms = 2
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go s.Serve(ln)
	a := dial(t, ln.Addr().String(), "NPWR00001_00", "alice", 3658)
	for i := 0; i < 2; i++ {
		if rt, _ := a.call(msgCreateRoom, createBody(nil)); rt != msgRoomJoined {
			t.Fatalf("room %d: got 0x%02X", i, rt)
		}
	}
	rt, r := a.call(msgCreateRoom, createBody(nil))
	if rt != msgError || r.u32() != errLimitReached {
		t.Fatalf("third room: got 0x%02X, want LIMIT_REACHED", rt)
	}
	s.mu.Lock()
	s.maxPlayerRooms = 0 // no limit
	s.mu.Unlock()
	if rt, _ := a.call(msgCreateRoom, createBody(nil)); rt != msgRoomJoined {
		t.Fatalf("room with no limit: got 0x%02X", rt)
	}
}

func TestConnLimitPerAddr(t *testing.T) {
	s := NewServer(10, false)
	s.maxPerAddr = 2
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go s.Serve(ln)
	addr := ln.Addr().String()
	dial(t, addr, "NPWR00001_00", "alice", 3658)
	dial(t, addr, "NPWR00001_00", "bob", 3659)

	conn, err := net.Dial("tcp", addr) // the third is closed without an answer
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := readPacket(conn); err == nil {
		t.Fatal("third connection was served")
	}
}

func TestRequestRateLimit(t *testing.T) {
	s := NewServer(10, false)
	s.maxReqRate = 5
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go s.Serve(ln)
	a := dial(t, ln.Addr().String(), "NPWR00001_00", "alice", 3658) // request 1
	for i := 0; i < 3; i++ {                                        // under the limit: still served
		if rt, _ := a.call(msgSearchRooms, []byte{0, 0, 0, 10}); rt != msgRoomList {
			t.Fatalf("request %d: got 0x%02X", i+2, rt)
		}
	}
	for i := 0; i < 5; i++ { // over it: the connection is dropped
		w := &writer{}
		w.u32(100 + uint32(i))
		writePacket(a.conn, msgHeartbeat, w.b)
	}
	a.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := readPacket(a.conn); err == nil {
		t.Fatal("connection stayed open past the rate limit")
	}
}
