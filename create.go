package main

import (
	"fmt"
	"net/http"
	"time"
)

// POST /requests : créer une demande (tout utilisateur connecté).
// Le demandeur est l'utilisateur connecté : on ne lit pas "requester" dans le corps.
func createRequest(w http.ResponseWriter, r *http.Request, u User) {
	var req Request
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Requester = u.Name

	// Contrôle de saisie : un message par champ en erreur
	if errs := cleanAndValidate(&req); len(errs) > 0 {
		writeErrors(w, http.StatusBadRequest, errs)
		return
	}

	now := time.Now()
	result, err := db.Exec(
		`INSERT INTO requests (title, description, requester, requester_id, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		req.Title, req.Description, req.Requester, u.ID, StatusOpen, now, now,
	)
	if err != nil {
		writeErrors(w, http.StatusInternalServerError, map[string]string{
			"general": "Erreur du serveur : la demande n'a pas pu être enregistrée.",
		})
		return
	}
	id64, _ := result.LastInsertId()
	id := int(id64)

	notifyProcessors(u.ID, &id, fmt.Sprintf("Nouvelle demande #%d « %s » de %s.", id, req.Title, u.Name))

	created, err := getRequest(id)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}
