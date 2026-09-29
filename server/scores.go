package main

// NP Score-style leaderboards: one board per (title, board id), each user's
// best score kept, ties broken by who got there first (as NP Score ranks).
// The sort order comes from the first RECORD_SCORE for a board. On PSN it
// is server-side board config the title never sends; here the client passes
// the order it knows from the title.

import (
	"sort"
	"time"
)

// ponytail: boards live in memory and go with the process. Persist them
// (a JSON snapshot on change) once someone wants a board to outlive a restart.

type boardKey struct {
	commID string
	id     uint32
}

type board struct {
	ascending bool // lower is better (lap times)
	entries   map[string]*scoreEntry
}

type scoreEntry struct {
	onlineID string
	score    int64
	comment  []byte
	at       time.Time
}

func (b *board) better(a, c *scoreEntry) bool {
	if a.score != c.score {
		if b.ascending {
			return a.score < c.score
		}
		return a.score > c.score
	}
	return a.at.Before(c.at)
}

func (b *board) ranked() []*scoreEntry {
	out := make([]*scoreEntry, 0, len(b.entries))
	for _, e := range b.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return b.better(out[i], out[j]) })
	return out
}

func (b *board) rankOf(onlineID string) uint32 {
	for i, e := range b.ranked() {
		if e.onlineID == onlineID {
			return uint32(i + 1)
		}
	}
	return 0
}

// RECORD_SCORE: board u32 | order u8 (0 higher-is-better, 1 lower) | score s64 | comment blob
// -> SCORE_RECORDED: req | rank u32 (the user's rank after recording; their
// best stands if this one is worse)
func (s *Server) recordScore(c *client, req uint32, rd *reader) (byte, []byte) {
	id, order, score, comment := rd.u32(), rd.u8(), int64(rd.u64()), rd.blob()
	if rd.err != nil || len(comment) > 64 {
		return msgError, errReply(req, errBadRequest)
	}
	k := boardKey{c.commID, id}
	b := s.boards[k]
	if b == nil {
		b = &board{ascending: order == 1, entries: map[string]*scoreEntry{}}
		s.boards[k] = b
	}
	e := &scoreEntry{onlineID: c.onlineID, score: score, comment: comment, at: time.Now()}
	if old := b.entries[c.onlineID]; old == nil || b.better(e, old) {
		b.entries[c.onlineID] = e
	}
	w := &writer{}
	w.u32(req)
	w.u32(b.rankOf(c.onlineID))
	return msgScoreRecorded, w.b
}

// RANKING: req | total u32 | count u16 | entries...
// entry: rank u32 | online_id [16] | score s64 | comment blob | recorded unix u64
// rank 0 means "no score" (only from GET_RANKING_BY_ID).
func rankingReply(req uint32, total int, rows []*scoreEntry, ranks []uint32, ids []string) []byte {
	w := &writer{}
	w.u32(req)
	w.u32(uint32(total))
	w.u16(uint16(len(rows)))
	for i, e := range rows {
		w.u32(ranks[i])
		if e == nil {
			w.str(ids[i], 16)
			w.u64(0)
			w.blob(nil)
			w.u64(0)
			continue
		}
		w.str(e.onlineID, 16)
		w.u64(uint64(e.score))
		w.blob(e.comment)
		w.u64(uint64(e.at.Unix()))
	}
	return w.b
}

// GET_RANKING: board u32 | start u32 (1-based rank) | count u16
func (s *Server) getRanking(c *client, req uint32, rd *reader) (byte, []byte) {
	id, start, count := rd.u32(), rd.u32(), rd.u16()
	if rd.err != nil || start == 0 || count > 100 {
		return msgError, errReply(req, errBadRequest)
	}
	var all []*scoreEntry
	if b := s.boards[boardKey{c.commID, id}]; b != nil {
		all = b.ranked()
	}
	lo := min(int(start-1), len(all))
	rows := all[lo:min(lo+int(count), len(all))]
	ranks := make([]uint32, len(rows))
	for i := range rows {
		ranks[i] = uint32(lo + i + 1)
	}
	return msgRanking, rankingReply(req, len(all), rows, ranks, nil)
}

// GET_RANKING_BY_ID: board u32 | n u16 | online_id [16] * n (friends' ranks)
func (s *Server) getRankingByID(c *client, req uint32, rd *reader) (byte, []byte) {
	id, n := rd.u32(), rd.u16()
	ids := make([]string, n)
	for i := range ids {
		ids[i] = rd.str(16)
	}
	if rd.err != nil || n > 100 {
		return msgError, errReply(req, errBadRequest)
	}
	b := s.boards[boardKey{c.commID, id}]
	rows := make([]*scoreEntry, n)
	ranks := make([]uint32, n)
	total := 0
	if b != nil {
		total = len(b.entries)
		for i, who := range ids {
			if e := b.entries[who]; e != nil {
				rows[i], ranks[i] = e, b.rankOf(who)
			}
		}
	}
	return msgRanking, rankingReply(req, total, rows, ranks, ids)
}
