package main

import (
	"log"
	"net/http"
	"time"
)

type Notification struct {
	ID        int       `json:"id"`
	RequestID *int      `json:"request_id"`
	Message   string    `json:"message"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

// notify envoie une notification à un utilisateur.
// Une notification qui échoue ne doit pas faire échouer l'action principale :
// on note seulement l'erreur dans les journaux.
func notify(userID int, requestID *int, message string) {
	_, err := db.Exec(
		`INSERT INTO notifications (user_id, request_id, message, created_at) VALUES (?, ?, ?, ?)`,
		userID, requestID, message, time.Now())
	if err != nil {
		log.Println("notification non enregistrée :", err)
	}
}

// notifyProcessors prévient tous les agents et admins, sauf l'auteur de l'action
func notifyProcessors(exceptID int, requestID *int, message string) {
	rows, err := db.Query(`SELECT id FROM users WHERE role IN (?, ?) AND id != ? AND active = 1`,
		RoleAgent, RoleAdmin, exceptID)
	if err != nil {
		log.Println("notification non envoyée :", err)
		return
	}
	var ids []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close() // on ferme avant d'écrire (une seule connexion SQLite)
	for _, id := range ids {
		notify(id, requestID, message)
	}
}

// notifyUnlessActor notifie un utilisateur, sauf si c'est lui qui a fait l'action
func notifyUnlessActor(userID *int, actor User, requestID *int, message string) {
	if userID != nil && *userID != actor.ID {
		notify(*userID, requestID, message)
	}
}

// GET /notifications : les 30 dernières notifications + le nombre de non lues
func listNotifications(w http.ResponseWriter, r *http.Request, u User) {
	rows, err := db.Query(
		`SELECT id, request_id, message, is_read, created_at FROM notifications
		 WHERE user_id = ? ORDER BY id DESC LIMIT 30`, u.ID)
	if err != nil {
		serverError(w)
		return
	}
	defer rows.Close()

	list := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.RequestID, &n.Message, &n.Read, &n.CreatedAt); err != nil {
			serverError(w)
			return
		}
		list = append(list, n)
	}
	rows.Close()

	var unread int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id = ? AND is_read = 0`, u.ID).Scan(&unread); err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": unread, "items": list})
}

// POST /notifications/read : tout marquer comme lu
func markNotificationsRead(w http.ResponseWriter, r *http.Request, u User) {
	if _, err := db.Exec(`UPDATE notifications SET is_read = 1 WHERE user_id = ?`, u.ID); err != nil {
		serverError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
