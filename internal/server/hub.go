package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// The WebSocket at /ws?project=<id> is how an editor page follows a project.
// The server sends the document when the page connects and again after every
// change, whoever made it; the page sends the commands a person's gestures
// turn into and gets each one's result back:
//
//   server → page   {"type":"hello","origin":"c7","document":…,"revision":n,"history":{…}}
//   server → page   {"type":"doc","document":…,"revision":n,"origin":"c7","command":{…},"history":{…}}
//   page → server   {"type":"command","reqId":"r1","command":{…}}
//   server → page   {"type":"result","reqId":"r1","ok":true,"data":…,"document":…,"revision":n,"history":{…}}
//
// "origin" names the connection a change came from (empty for the CLI and
// the HTTP API), so a page can tell its own edits from everyone else's.

// Envelope is the JSON frame used in both directions on /ws.
type Envelope struct {
	Type     string          `json:"type"`
	ReqID    string          `json:"reqId,omitempty"`
	OK       bool            `json:"ok,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Error    string          `json:"error,omitempty"`
	Document json.RawMessage `json:"document,omitempty"`
	Revision int64           `json:"revision,omitempty"`
	Command  json.RawMessage `json:"command,omitempty"`
	Origin   string          `json:"origin,omitempty"`
	History  *History        `json:"history,omitempty"`
	Message  string          `json:"message,omitempty"`
}

type conn struct {
	id      string
	ws      *websocket.Conn
	project string
	writeMu sync.Mutex
	closed  bool
}

func (c *conn) send(env Envelope) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return errors.New("connection closed")
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteJSON(env)
}

func (c *conn) close() {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if !c.closed {
		c.closed = true
		_ = c.ws.Close()
	}
}

// Hub tracks which pages are showing which project.
type Hub struct {
	srv    *Server
	mu     sync.RWMutex
	rooms  map[string]map[*conn]bool
	upgrad websocket.Upgrader
	seq    atomic.Int64
}

func newHub(srv *Server) *Hub {
	return &Hub{
		srv:   srv,
		rooms: map[string]map[*conn]bool{},
		upgrad: websocket.Upgrader{
			ReadBufferSize:  16 * 1024,
			WriteBufferSize: 64 * 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
}

// Connected counts the pages showing a project.
func (h *Hub) Connected(id string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms[id])
}

// broadcast sends an envelope to every page showing a project.
func (h *Hub) broadcast(projectID string, env Envelope) {
	h.mu.RLock()
	conns := make([]*conn, 0, len(h.rooms[projectID]))
	for c := range h.rooms[projectID] {
		conns = append(conns, c)
	}
	h.mu.RUnlock()
	for _, c := range conns {
		_ = c.send(env)
	}
}

// ServeWS attaches a page to a project.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project")
	if projectID == "" {
		http.Error(w, "missing ?project=", http.StatusBadRequest)
		return
	}
	e, err := h.srv.engine(projectID)
	if err != nil {
		http.Error(w, fmt.Sprintf("load project: %v", err), http.StatusNotFound)
		return
	}
	ws, err := h.upgrad.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(64 << 20)
	c := &conn{id: fmt.Sprintf("c%d", h.seq.Add(1)), ws: ws, project: projectID}
	h.mu.Lock()
	if h.rooms[projectID] == nil {
		h.rooms[projectID] = map[*conn]bool{}
	}
	h.rooms[projectID][c] = true
	h.mu.Unlock()

	var rev int64
	if p, _ := h.srv.store.GetProject(projectID); p != nil {
		rev = p.Revision
	}
	_ = c.send(Envelope{Type: "hello", Origin: c.id, Document: e.Snapshot(), Revision: rev, History: historyOf(e)})
	go h.readLoop(c)
}

func (h *Hub) readLoop(c *conn) {
	defer func() {
		h.mu.Lock()
		delete(h.rooms[c.project], c)
		if len(h.rooms[c.project]) == 0 {
			delete(h.rooms, c.project)
		}
		h.mu.Unlock()
		c.close()
	}()
	for {
		var env Envelope
		if err := c.ws.ReadJSON(&env); err != nil {
			return
		}
		switch env.Type {
		case "command":
			res, err := h.srv.ExecFrom(c.project, env.Command, c.id)
			reply := Envelope{Type: "result", ReqID: env.ReqID}
			if err != nil {
				reply.Error = err.Error()
			} else {
				reply.OK, reply.Data, reply.Error = res.OK, res.Data, res.Error
				reply.Document, reply.Revision, reply.History = res.Document, res.Revision, res.History
			}
			if err := c.send(reply); err != nil {
				log.Printf("[hub] project %s: reply lost: %v", c.project, err)
			}
		case "ping":
			_ = c.send(Envelope{Type: "pong", ReqID: env.ReqID})
		}
	}
}
