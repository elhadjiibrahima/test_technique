package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const commentMax = 1000

// GET /requests/{id}/comments : le fil des réponses d'une demande, du plus ancien au plus récent.
// Visible par ceux qui peuvent voir la demande (le demandeur, les agents, les admins).
func listComments(w http.ResponseWriter, r *http.Request, u User) {
	req, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}

	rows, err := db.Query(
		`SELECT id, request_id, author_id, author_name, author_role, message, created_at
		 FROM comments WHERE request_id = ? ORDER BY id`, req.ID)
	if err != nil {
		serverError(w)
		return
	}
	defer rows.Close()

	list := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.RequestID, &c.AuthorID, &c.AuthorName, &c.AuthorRole, &c.Message, &c.CreatedAt); err != nil {
			serverError(w)
			return
		}
		list = append(list, c)
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /requests/{id}/comments : répondre sur une demande.
//   - un admin ou un agent répond au demandeur → le demandeur est notifié
//   - le demandeur répond → l'agent qui traite la demande est notifié
//     (ou, si personne ne la traite, tous les agents et admins)
func addComment(w http.ResponseWriter, r *http.Request, u User) {
	req, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}

	var body struct {
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Message = strings.TrimSpace(body.Message)
	switch n := utf8.RuneCountInString(body.Message); {
	case n == 0:
		writeErrors(w, http.StatusBadRequest, map[string]string{"message": "La réponse est vide."})
		return
	case n > commentMax:
		writeErrors(w, http.StatusBadRequest, map[string]string{"message": "La réponse est trop longue (1000 caractères maximum)."})
		return
	}

	c := Comment{
		RequestID:  req.ID,
		AuthorID:   &u.ID,
		AuthorName: u.Name,
		AuthorRole: u.Role,
		Message:    body.Message,
		CreatedAt:  time.Now(),
	}
	res, err := db.Exec(
		`INSERT INTO comments (request_id, author_id, author_name, author_role, message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		c.RequestID, u.ID, c.AuthorName, c.AuthorRole, c.Message, c.CreatedAt)
	if err != nil {
		serverError(w)
		return
	}
	id, _ := res.LastInsertId()
	c.ID = int(id)

	// La demande a bougé : on met à jour sa date
	db.Exec(`UPDATE requests SET updated_at = ? WHERE id = ?`, c.CreatedAt, req.ID)
	liveRequestChanged(req.ID, req.RequesterID)

	extrait := excerpt(c.Message, 80)
	if isOwner(u, req) {
		msg := fmt.Sprintf("%s a répondu sur la demande #%d « %s » : %s", u.Name, req.ID, req.Title, extrait)
		if req.AssigneeID != nil {
			notifyUnlessActor(req.AssigneeID, u, &req.ID, msg)
		} else {
			notifyProcessors(u.ID, &req.ID, msg)
		}
	} else {
		notifyUnlessActor(req.RequesterID, u, &req.ID,
			fmt.Sprintf("%s a répondu à votre demande #%d « %s » : %s", u.Name, req.ID, req.Title, extrait))
	}

	writeJSON(w, http.StatusCreated, c)
}

// excerpt coupe un texte trop long pour une notification
func excerpt(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ") // une seule ligne
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return string([]rune(text)[:max]) + "…"
}
