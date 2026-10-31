package main

import (
	"log"
	"net/http"
	"time"
)

// Event : une ligne de l'historique d'une demande
type Event struct {
	ID        int       `json:"id"`
	ActorName string    `json:"actor_name"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// logEvent ajoute une ligne à l'historique d'une demande.
// Exemple : logEvent(12, u, "a changé le statut : ouverte → en cours")
// Comme pour les notifications, une erreur ici ne fait pas échouer l'action.
func logEvent(requestID int, actor User, message string) {
	_, err := db.Exec(
		`INSERT INTO request_events (request_id, actor_id, actor_name, message, created_at) VALUES (?, ?, ?, ?, ?)`,
		requestID, actor.ID, actor.Name, message, time.Now())
	if err != nil {
		log.Println("historique non enregistré :", err)
	}
	// Toute action notée dans l'historique change la demande : on prévient les pages
	liveRequestChangedByID(requestID)
}

// GET /requests/{id}/history : l'historique d'une demande, du plus ancien au plus récent
func listHistory(w http.ResponseWriter, r *http.Request, u User) {
	req, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}
	rows, err := db.Query(
		`SELECT id, actor_name, message, created_at FROM request_events WHERE request_id = ? ORDER BY id`, req.ID)
	if err != nil {
		serverError(w)
		return
	}
	defer rows.Close()

	list := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ActorName, &e.Message, &e.CreatedAt); err != nil {
			serverError(w)
			return
		}
		list = append(list, e)
	}
	writeJSON(w, http.StatusOK, list)
}
