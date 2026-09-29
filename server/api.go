package main

// Read-only HTTP view of the server: JSON for scripts, one HTML page for
// people. Bind it to localhost or a LAN address; it has no auth and shows
// every player's online ID.

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"time"
)

type roomJSON struct {
	ID      uint64   `json:"id"`
	Title   string   `json:"title"`
	Members []string `json:"members"`
	Max     uint8    `json:"max"`
	Age     string   `json:"age"`
}

type boardJSON struct {
	Title   string `json:"title"`
	Board   uint32 `json:"board"`
	Entries int    `json:"entries"`
	Leader  string `json:"leader"`
	Best    int64  `json:"best"`
}

type statsJSON struct {
	Uptime      string      `json:"uptime"`
	Clients     int         `json:"clients"`
	Connections uint64      `json:"connections_total"`
	Rooms       []roomJSON  `json:"rooms"`
	Boards      []boardJSON `json:"boards"`
}

func (s *Server) snapshot() statsJSON {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := statsJSON{
		Uptime:      time.Since(s.started).Round(time.Second).String(),
		Clients:     len(s.clients),
		Connections: s.conns,
		Rooms:       []roomJSON{},
		Boards:      []boardJSON{},
	}
	for _, r := range s.rooms {
		rj := roomJSON{ID: r.id, Title: r.commID, Max: r.max,
			Age: time.Since(r.created).Round(time.Second).String()}
		for _, m := range r.members {
			rj.Members = append(rj.Members, m.c.onlineID)
		}
		st.Rooms = append(st.Rooms, rj)
	}
	for k, b := range s.boards {
		bj := boardJSON{Title: k.commID, Board: k.id, Entries: len(b.entries)}
		if r := b.ranked(); len(r) > 0 {
			bj.Leader, bj.Best = r[0].onlineID, r[0].score
		}
		st.Boards = append(st.Boards, bj)
	}
	sort.Slice(st.Rooms, func(i, j int) bool { return st.Rooms[i].ID < st.Rooms[j].ID })
	sort.Slice(st.Boards, func(i, j int) bool {
		a, b := st.Boards[i], st.Boards[j]
		return a.Title < b.Title || a.Title == b.Title && a.Board < b.Board
	})
	return st
}

var page = template.Must(template.New("p").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="viewport" content="width=device-width">
<meta http-equiv="refresh" content="5"><title>psnr</title>
<style>body{font:14px system-ui;margin:16px;max-width:900px}table{border-collapse:collapse;width:100%;margin-bottom:24px}
td,th{text-align:left;padding:4px 8px;border-bottom:1px solid #8884}</style>
<h1>psnr</h1><p>up {{.Uptime}} · {{.Clients}} connected · {{.Connections}} connections total</p>
<h2>Rooms</h2><table><tr><th>ID<th>Title<th>Members<th>Age</tr>
{{range .Rooms}}<tr><td>{{.ID}}<td>{{.Title}}<td>{{len .Members}}/{{.Max}} {{range .Members}}{{.}} {{end}}<td>{{.Age}}</tr>{{else}}<tr><td colspan=4>none</tr>{{end}}</table>
<h2>Leaderboards</h2><table><tr><th>Title<th>Board<th>Entries<th>Leader</tr>
{{range .Boards}}<tr><td>{{.Title}}<td>{{.Board}}<td>{{.Entries}}<td>{{.Leader}} ({{.Best}})</tr>{{else}}<tr><td colspan=4>none</tr>{{end}}</table>`))

func (s *Server) HTTP() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.snapshot())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := page.Execute(w, s.snapshot()); err != nil {
			fmt.Fprintln(w, err)
		}
	})
	return mux
}
