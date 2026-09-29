package main

// Matching2-style rooms. A room belongs to one title (communication ID) and
// holds up to max members. Members are numbered from 1 in join order, the way
// Matching2 room member IDs are, and the owner is a member ID. Every join or
// leave pushes to the members who stay, and each member entry carries the
// peer's address and P2P port: that is what the client's signaling layer
// connects to. The server never carries game traffic.

import (
	"encoding/binary"
	"time"
)

type room struct {
	id         uint64
	commID     string
	max        uint8
	flags      uint32
	external   []byte // visible to searchers (Matching2 "external" bin attrs)
	internal   []byte // visible to members only
	owner      uint16
	members    []*member
	nextMember uint16
	created    time.Time
}

type member struct {
	id   uint16
	c    *client
	data []byte // the member's own data (Matching2 member bin attrs), set at join
}

// Member entry on the wire:
// member_id u16 | user_id u32 | online_id [16] | ip [4] | p2p_port u16 | owner u8 | data blob
func (r *room) putMember(w *writer, m *member) {
	w.u16(m.id)
	w.u32(m.c.id)
	w.str(m.c.onlineID, 16)
	w.bytes(m.c.ip[:])
	w.u16(m.c.p2pPort)
	if m.id == r.owner {
		w.u8(1)
	} else {
		w.u8(0)
	}
	w.blob(m.data)
}

func (r *room) memberOf(c *client) *member {
	for _, m := range r.members {
		if m.c == c {
			return m
		}
	}
	return nil
}

// ROOM_JOINED: req | room_id u64 | my_member u16 | owner u16 | max u8 |
// flags u32 | external blob | internal blob | count u8 | members...
func (r *room) joinedReply(req uint32, me *member) []byte {
	w := &writer{}
	w.u32(req)
	w.u64(r.id)
	w.u16(me.id)
	w.u16(r.owner)
	w.u8(r.max)
	w.u32(r.flags)
	w.blob(r.external)
	w.blob(r.internal)
	w.u8(uint8(len(r.members)))
	for _, m := range r.members {
		r.putMember(w, m)
	}
	return w.b
}

func (s *Server) addMember(r *room, c *client, data []byte) *member {
	r.nextMember++
	m := &member{id: r.nextMember, c: c, data: data}
	r.members = append(r.members, m)
	c.rooms[r.id] = r
	return m
}

func (s *Server) createRoom(c *client, req uint32, rd *reader) (byte, []byte) {
	max, flags, ext, in, mine := rd.u8(), rd.u32(), rd.blob(), rd.blob(), rd.blob()
	if rd.err != nil || max == 0 {
		return msgError, errReply(req, errBadRequest)
	}
	if len(s.rooms) >= s.maxRooms {
		return msgError, errReply(req, errLimitReached)
	}
	s.nextRoom++
	r := &room{id: s.nextRoom, commID: c.commID, max: max, flags: flags,
		external: ext, internal: in, created: time.Now()}
	s.rooms[r.id] = r
	me := s.addMember(r, c, mine)
	r.owner = me.id
	s.logf("room %d created by %s (%s, max %d)", r.id, c.onlineID, c.commID, max)
	return msgRoomJoined, r.joinedReply(req, me)
}

// ROOM_LIST: req | total u16 | count u16 | entries...
// entry: room_id u64 | owner online_id [16] | cur u8 | max u8 | flags u32 | external blob
func (s *Server) searchRooms(c *client, req uint32, rd *reader) (byte, []byte) {
	start, max := rd.u16(), rd.u16()
	if rd.err != nil {
		return msgError, errReply(req, errBadRequest)
	}
	var open []*room
	for _, r := range s.sortedRooms(c.commID) {
		if len(r.members) < int(r.max) {
			open = append(open, r)
		}
	}
	page := open[min(int(start), len(open)):]
	page = page[:min(int(max), len(page))]

	w := &writer{}
	w.u32(req)
	w.u16(uint16(len(open)))
	w.u16(uint16(len(page)))
	for _, r := range page {
		w.u64(r.id)
		owner := ""
		for _, m := range r.members {
			if m.id == r.owner {
				owner = m.c.onlineID
			}
		}
		w.str(owner, 16)
		w.u8(uint8(len(r.members)))
		w.u8(r.max)
		w.u32(r.flags)
		w.blob(r.external)
	}
	return msgRoomList, w.b
}

func (s *Server) joinRoom(c *client, req uint32, rd *reader) (byte, []byte, []push) {
	id, mine := rd.u64(), rd.blob()
	r, ok := s.rooms[id]
	switch {
	case rd.err != nil:
		return msgError, errReply(req, errBadRequest), nil
	case !ok || r.commID != c.commID:
		return msgError, errReply(req, errNotFound), nil
	case r.memberOf(c) != nil:
		return msgRoomJoined, r.joinedReply(req, r.memberOf(c)), nil
	case len(r.members) >= int(r.max):
		return msgError, errReply(req, errRoomFull), nil
	}
	me := s.addMember(r, c, mine)

	w := &writer{}
	w.u64(r.id)
	r.putMember(w, me)
	var out []push
	for _, m := range r.members {
		if m != me {
			out = append(out, push{m.c, msgMemberJoined, w.b})
		}
	}
	s.logf("room %d: %s joined as member %d", r.id, c.onlineID, me.id)
	return msgRoomJoined, r.joinedReply(req, me), out
}

func (s *Server) leaveRoom(c *client, req uint32, rd *reader) (byte, []byte, []push) {
	r, ok := s.rooms[rd.u64()]
	if rd.err != nil || !ok || r.memberOf(c) == nil {
		return msgError, errReply(req, errNotInRoom), nil
	}
	return msgOK, okReply(req), s.leave(r, c)
}

// leave removes c from r and returns the MEMBER_LEFT pushes for those who
// stay. The owner leaving hands the room to the longest-standing member;
// the last one out closes it.
func (s *Server) leave(r *room, c *client) []push {
	gone := r.memberOf(c)
	if gone == nil {
		return nil
	}
	delete(c.rooms, r.id)
	for i, m := range r.members {
		if m == gone {
			r.members = append(r.members[:i], r.members[i+1:]...)
			break
		}
	}
	if len(r.members) == 0 {
		delete(s.rooms, r.id)
		s.logf("room %d closed", r.id)
		return nil
	}
	if r.owner == gone.id {
		r.owner = r.members[0].id
	}

	// MEMBER_LEFT: room_id u64 | member_id u16 | owner u16
	w := &writer{}
	w.u64(r.id)
	w.u16(gone.id)
	w.u16(r.owner)
	var out []push
	for _, m := range r.members {
		out = append(out, push{m.c, msgMemberLeft, w.b})
	}
	return out
}

// SET_ROOM_DATA: room_id u64 | which u8 | blob. which 0 = external data,
// 1 = internal data, 2 = flags (the blob is a u32; searchers see them in
// ROOM_LIST, so a title can mark its room closed). Only the owner writes;
// members get ROOM_DATA: room_id | which | blob.
func (s *Server) setRoomData(c *client, req uint32, rd *reader) (byte, []byte, []push) {
	r, ok := s.rooms[rd.u64()]
	which, data := rd.u8(), rd.blob()
	switch {
	case rd.err != nil || which > 2 || which == 2 && len(data) != 4:
		return msgError, errReply(req, errBadRequest), nil
	case !ok || r.memberOf(c) == nil:
		return msgError, errReply(req, errNotInRoom), nil
	case r.memberOf(c).id != r.owner:
		return msgError, errReply(req, errNotOwner), nil
	}
	switch which {
	case 0:
		r.external = data
	case 1:
		r.internal = data
	case 2:
		r.flags = binary.BigEndian.Uint32(data)
	}
	w := &writer{}
	w.u64(r.id)
	w.u8(which)
	w.blob(data)
	var out []push
	for _, m := range r.members {
		if m.c != c {
			out = append(out, push{m.c, msgRoomData, w.b})
		}
	}
	return msgOK, okReply(req), out
}

// ROOM_MESSAGE: room_id u64 | to u16 (0 = everyone else) | blob.
// Delivered as ROOM_MSG: room_id | from u16 | to u16 | blob.
func (s *Server) roomMessage(c *client, req uint32, rd *reader) (byte, []byte, []push) {
	r, ok := s.rooms[rd.u64()]
	to, data := rd.u16(), rd.blob()
	if rd.err != nil {
		return msgError, errReply(req, errBadRequest), nil
	}
	if !ok || r.memberOf(c) == nil {
		return msgError, errReply(req, errNotInRoom), nil
	}
	from := r.memberOf(c)
	w := &writer{}
	w.u64(r.id)
	w.u16(from.id)
	w.u16(to)
	w.blob(data)
	var out []push
	for _, m := range r.members {
		if m != from && (to == 0 || m.id == to) {
			out = append(out, push{m.c, msgRoomMsg, w.b})
		}
	}
	return msgOK, okReply(req), out
}

// KICK_MEMBER: room_id u64 | member_id u16. Owner only; the target gets
// KICKED: room_id, the rest MEMBER_LEFT.
func (s *Server) kick(c *client, req uint32, rd *reader) (byte, []byte, []push) {
	r, ok := s.rooms[rd.u64()]
	target := rd.u16()
	if rd.err != nil {
		return msgError, errReply(req, errBadRequest), nil
	}
	if !ok || r.memberOf(c) == nil {
		return msgError, errReply(req, errNotInRoom), nil
	}
	if r.memberOf(c).id != r.owner {
		return msgError, errReply(req, errNotOwner), nil
	}
	for _, m := range r.members {
		if m.id == target && m.c != c {
			w := &writer{}
			w.u64(r.id)
			out := append([]push{{m.c, msgKicked, w.b}}, s.leave(r, m.c)...)
			return msgOK, okReply(req), out
		}
	}
	return msgError, errReply(req, errNotFound), nil
}
