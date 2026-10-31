package main

import (
	"fmt"
	"net/http"
	"time"
)

// POST /requests : créer une demande (tout utilisateur connecté).
// Le demandeur est l'utilisateur connecté : on ne lit pas "requester" dans le corps.
// "priority" (low, normal, urgent) et "category" sont optionnels.
func createRequest(w http.ResponseWriter, r *http.Request, u User) {
	var req Request
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Requester = u.Name

	// Contrôle de saisie : un message par champ en erreur
	errs := cleanAndValidate(&req)
	normalizeMeta(&req, errs)
	if len(errs) > 0 {
		writeErrors(w, http.StatusBadRequest, errs)
		return
	}

	now := time.Now()
	result, err := db.Exec(
		`INSERT INTO requests (title, description, requester, requester_id, status, priority, category, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Title, req.Description, req.Requester, u.ID, StatusOpen, req.Priority, req.Category, now, now,
	)
	if err != nil {
		writeErrors(w, http.StatusInternalServerError, map[string]string{
			"general": "Erreur du serveur : la demande n'a pas pu être enregistrée.",
		})
		return
	}
	id64, _ := result.LastInsertId()
	id := int(id64)

	logEvent(id, u, fmt.Sprintf("a créé la demande (priorité %s, %s)",
		priorityLabels[req.Priority], categoryLabels[req.Category]))

	urgent := ""
	if req.Priority == PriorityUrgent {
		urgent = "🔴 URGENT — "
	}
	notifyProcessors(u.ID, &id, fmt.Sprintf("%sNouvelle demande #%d « %s » de %s.", urgent, id, req.Title, u.Name))

	created, err := getRequest(id)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}
