package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Temps réel : chaque page ouverte garde une connexion WebSocket sur /ws.
// Quand quelque chose change, le serveur prévient les pages concernées,
// qui se mettent à jour toutes seules (sans recharger).
//
// Messages envoyés (JSON) :
//   {"type": "notification", "message": "...", "request_id": 12}  nouvelle notification pour cet utilisateur
//   {"type": "request", "request_id": 12}                         une demande visible a changé
//   {"type": "users"}                                             la liste des comptes a changé (admins)
//   {"type": "presence", "user_id": 3, "online": true}            quelqu'un se connecte / se déconnecte (admins)
//   {"type": "me"}                                                mon compte a changé (rôle, fiche) : recharger
//   {"type": "logout", "message": "..."}                          mon compte est désactivé ou supprimé

const (
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 60 * time.Second // sans réponse au ping pendant 60 s, la connexion est fermée
	wsPingPeriod = 25 * time.Second // les hébergeurs coupent souvent les connexions inactives
	wsSendBuffer = 32
)

type liveEvent struct {
	Type      string `json:"type"`
	RequestID *int   `json:"request_id,omitempty"`
	UserID    *int   `json:"user_id,omitempty"`
	Online    *bool  `json:"online,omitempty"`
	Message   string `json:"message,omitempty"`
}

type liveClient struct {
	conn   *websocket.Conn
	send   chan []byte
	userID int
	role   Role
}

type liveHub struct {
	mu      sync.RWMutex
	clients map[*liveClient]struct{}
}

var hub = &liveHub{clients: map[*liveClient]struct{}{}}

// sendTo envoie l'événement aux connexions choisies par le filtre.
// Ne bloque jamais : si une page ne suit pas, le message est abandonné
// (elle se resynchronisera au prochain événement).
func (h *liveHub) sendTo(ev liveEvent, keep func(c *liveClient) bool) {
	data, _ := json.Marshal(ev)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if keep(c) {
			select {
			case c.send <- data:
			default:
			}
		}
	}
}

func (h *liveHub) sendToUser(userID int, ev liveEvent) {
	h.sendTo(ev, func(c *liveClient) bool { return c.userID == userID })
}

func (h *liveHub) sendToAdmins(ev liveEvent) {
	h.sendTo(ev, func(c *liveClient) bool { return c.role == RoleAdmin })
}

// isOnline : l'utilisateur a-t-il au moins une page ouverte ?
func (h *liveHub) isOnline(userID int) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.userID == userID {
			return true
		}
	}
	return false
}

// setRole met à jour le rôle des connexions d'un utilisateur (pour filtrer les messages)
func (h *liveHub) setRole(userID int, role Role) {
	h.mu.Lock()
	for c := range h.clients {
		if c.userID == userID {
			c.role = role
		}
	}
	h.mu.Unlock()
}

func (h *liveHub) add(c *liveClient) (first bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	first = true
	for o := range h.clients {
		if o.userID == c.userID {
			first = false
		}
	}
	h.clients[c] = struct{}{}
	return first
}

func (h *liveHub) remove(c *liveClient) (last bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; !ok {
		return false
	}
	delete(h.clients, c)
	close(c.send)
	for o := range h.clients {
		if o.userID == c.userID {
			return false
		}
	}
	return true
}

// ---------- Événements utilisés par le reste du code ----------

// liveRequestChanged prévient les pages qui voient cette demande :
// tous les agents et admins, plus le demandeur.
func liveRequestChanged(requestID int, requesterID *int) {
	id := requestID
	hub.sendTo(liveEvent{Type: "request", RequestID: &id}, func(c *liveClient) bool {
		return c.role.canProcess() || (requesterID != nil && c.userID == *requesterID)
	})
}

// liveRequestChangedByID : comme liveRequestChanged, en retrouvant le demandeur
func liveRequestChangedByID(requestID int) {
	var requesterID *int
	db.QueryRow(`SELECT requester_id FROM requests WHERE id = ?`, requestID).Scan(&requesterID)
	liveRequestChanged(requestID, requesterID)
}

func liveUsersChanged() {
	hub.sendToAdmins(liveEvent{Type: "users"})
}

// liveLogout déconnecte tout de suite les pages d'un utilisateur
func liveLogout(userID int, message string) {
	hub.sendToUser(userID, liveEvent{Type: "logout", Message: message})
}

// ---------- Connexion WebSocket ----------

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     sameOrigin,
}

// sameOrigin refuse les connexions ouvertes depuis un autre site : sinon, un site
// malveillant pourrait ouvrir un WebSocket avec le cookie de l'utilisateur.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

// GET /ws : ouvrir la connexion temps réel (il faut être connecté)
func liveSocket(w http.ResponseWriter, r *http.Request, u User) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade a déjà répondu avec une erreur
	}
	c := &liveClient{conn: conn, send: make(chan []byte, wsSendBuffer), userID: u.ID, role: u.Role}
	if hub.add(c) {
		online := true
		hub.sendToAdmins(liveEvent{Type: "presence", UserID: &u.ID, Online: &online})
	}

	go c.writeLoop()
	c.readLoop() // jusqu'à la fermeture

	if hub.remove(c) {
		online := false
		hub.sendToAdmins(liveEvent{Type: "presence", UserID: &u.ID, Online: &online})
	}
}

// readLoop lit les messages de la page (on n'en attend pas, à part les pongs)
func (c *liveClient) readLoop() {
	defer c.conn.Close()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) {
				log.Println("websocket :", err)
			}
			return
		}
	}
}

// writeLoop envoie les événements et un ping régulier
func (c *liveClient) writeLoop() {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case data, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
