package main

// Wire format. Every integer is big-endian, like the PS3 that the guests
// think they run on. The full message table lives in docs/api.md.
//
//	u8  type | u16 length | payload
//
// Every client request payload starts with a u32 request id. The reply echoes
// it, so a client can have several NP calls in flight at once; Matching2 and
// Score are asynchronous APIs, and titles fire requests before earlier ones
// answer. Server pushes (room events) carry no request id.

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	// client -> server
	msgHello        = 0x01
	msgHeartbeat    = 0x02
	msgCreateRoom   = 0x10
	msgSearchRooms  = 0x11
	msgJoinRoom     = 0x12
	msgLeaveRoom    = 0x13
	msgSetRoomData  = 0x14
	msgRoomMessage  = 0x15
	msgKickMember   = 0x16
	msgRecordScore  = 0x20
	msgGetRanking   = 0x21
	msgGetRankingID = 0x22

	// server -> client replies
	msgHelloAck      = 0x81
	msgRoomJoined    = 0x90
	msgRoomList      = 0x91
	msgOK            = 0x93
	msgScoreRecorded = 0xB0
	msgRanking       = 0xB1
	msgError         = 0x8F

	// server -> client pushes
	msgMemberJoined = 0xA1
	msgMemberLeft   = 0xA2
	msgRoomData     = 0xA3
	msgRoomMsg      = 0xA4
	msgKicked       = 0xA5
)

// Error codes in ERROR replies. Clients map them onto the SDK error the
// title expects; the server has no business knowing SDK values.
const (
	errBadRequest   = 1
	errNotFound     = 2
	errRoomFull     = 3
	errNotOwner     = 4
	errNotInRoom    = 5
	errLimitReached = 6
	errNoHello      = 7
)

const maxPayload = 0xFFFF

func readPacket(r io.Reader) (byte, []byte, error) {
	var hdr [3]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	p := make([]byte, binary.BigEndian.Uint16(hdr[1:]))
	if _, err := io.ReadFull(r, p); err != nil {
		return 0, nil, err
	}
	return hdr[0], p, nil
}

func writePacket(w io.Writer, t byte, p []byte) error {
	if len(p) > maxPayload {
		return errors.New("payload too large")
	}
	buf := make([]byte, 3+len(p))
	buf[0] = t
	binary.BigEndian.PutUint16(buf[1:], uint16(len(p)))
	copy(buf[3:], p)
	_, err := w.Write(buf)
	return err
}

// reader walks a payload. Any short read latches err, so handlers read every
// field and check once at the end.
type reader struct {
	b   []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil || len(r.b) < n {
		r.err = errors.New("truncated")
		return make([]byte, n)
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) u8() uint8   { return r.take(1)[0] }
func (r *reader) u16() uint16 { return binary.BigEndian.Uint16(r.take(2)) }
func (r *reader) u32() uint32 { return binary.BigEndian.Uint32(r.take(4)) }
func (r *reader) u64() uint64 { return binary.BigEndian.Uint64(r.take(8)) }

// blob is a u16 length followed by that many bytes, copied.
func (r *reader) blob() []byte { return append([]byte(nil), r.take(int(r.u16()))...) }

// str reads a fixed-width NUL-padded field.
func (r *reader) str(n int) string {
	b := r.take(n)
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

type writer struct{ b []byte }

func (w *writer) u8(v uint8)     { w.b = append(w.b, v) }
func (w *writer) u16(v uint16)   { w.b = binary.BigEndian.AppendUint16(w.b, v) }
func (w *writer) u32(v uint32)   { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *writer) u64(v uint64)   { w.b = binary.BigEndian.AppendUint64(w.b, v) }
func (w *writer) bytes(v []byte) { w.b = append(w.b, v...) }
func (w *writer) blob(v []byte)  { w.u16(uint16(len(v))); w.bytes(v) }

func (w *writer) str(s string, n int) {
	f := make([]byte, n)
	copy(f, s)
	w.bytes(f)
}
