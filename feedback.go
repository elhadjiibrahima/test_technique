package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// POST /requests/{id}/reopen : rouvrir une demande terminée (le demandeur, ou un admin).
// Corps : {"message": "Le problème est revenu ce matin"} — la raison est obligatoire
// et ajoutée aux réponses. La demande repart « en cours » chez le même agent,
// ou « ouverte » si personne ne la traitait.
func reopenRequest(w http.ResponseWriter, r *http.Request, u User) {
	req, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}
	if !isOwner(u, req) && u.Role != RoleAdmin {
		forbidden(w)
		return
	}
	if req.Status != StatusDone {
		writeErrors(w, http.StatusBadRequest, map[string]string{"general": "Seule une demande terminée peut être rouverte."})
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
		writeErrors(w, http.StatusBadRequest, map[string]string{"message": "Expliquez pourquoi vous rouvrez la demande."})
		return
	case n > commentMax:
		writeErrors(w, http.StatusBadRequest, map[string]string{"message": "Le message est trop long (1000 caractères maximum)."})
		return
	}

	newStatus := StatusOpen
	if req.AssigneeID != nil {
		newStatus = StatusInProgress
	}
	now := time.Now()
	// On efface aussi la note : elle portait sur le traitement précédent
	if _, err := db.Exec(
		`UPDATE requests SET status = ?, resolved_at = NULL, rating = NULL, rating_comment = '', updated_at = ? WHERE id = ?`,
		newStatus, now, req.ID); err != nil {
		serverError(w)
		return
	}
	db.Exec(`INSERT INTO comments (request_id, author_id, author_name, author_role, message, created_at)
	         VALUES (?, ?, ?, ?, ?, ?)`, req.ID, u.ID, u.Name, u.Role, "🔁 Demande rouverte : "+body.Message, now)

	logEvent(req.ID, u, "a rouvert la demande : "+excerpt(body.Message, 120))
	msg := fmt.Sprintf("%s a rouvert la demande #%d « %s » : %s", u.Name, req.ID, req.Title, excerpt(body.Message, 80))
	if req.AssigneeID != nil {
		notifyUnlessActor(req.AssigneeID, u, &req.ID, msg)
	} else {
		notifyProcessors(u.ID, &req.ID, msg)
	}
	if !isOwner(u, req) {
		notifyUnlessActor(req.RequesterID, u, &req.ID,
			fmt.Sprintf("Votre demande #%d « %s » a été rouverte par %s.", req.ID, req.Title, u.Name))
	}

	updated, err := getRequest(req.ID)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// POST /requests/{id}/rating : le demandeur note le traitement d'une demande terminée.
// Corps : {"rating": 4, "comment": "Rapide, merci"}. La note peut être changée.
func rateRequest(w http.ResponseWriter, r *http.Request, u User) {
	req, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}
	if !isOwner(u, req) {
		writeErrors(w, http.StatusForbidden, map[string]string{"general": "Seul le demandeur peut noter sa demande."})
		return
	}
	if req.Status != StatusDone {
		writeErrors(w, http.StatusBadRequest, map[string]string{"general": "On peut noter une demande une fois qu'elle est terminée."})
		return
	}

	var body struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Comment = strings.TrimSpace(body.Comment)
	if body.Rating < 1 || body.Rating > 5 {
		writeErrors(w, http.StatusBadRequest, map[string]string{"rating": "La note doit être entre 1 et 5."})
		return
	}
	if utf8.RuneCountInString(body.Comment) > 300 {
		writeErrors(w, http.StatusBadRequest, map[string]string{"comment": "Le commentaire est trop long (300 caractères maximum)."})
		return
	}

	if _, err := db.Exec(`UPDATE requests SET rating = ?, rating_comment = ? WHERE id = ?`,
		body.Rating, body.Comment, req.ID); err != nil {
		serverError(w)
		return
	}

	stars := strings.Repeat("★", body.Rating) + strings.Repeat("☆", 5-body.Rating)
	logEvent(req.ID, u, fmt.Sprintf("a noté le traitement %d/5", body.Rating))
	msg := fmt.Sprintf("%s a noté la demande #%d « %s » : %s", u.Name, req.ID, req.Title, stars)
	if body.Comment != "" {
		msg += " — " + excerpt(body.Comment, 80)
	}
	notifyUnlessActor(req.AssigneeID, u, &req.ID, msg)

	updated, err := getRequest(req.ID)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
