package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// POST /requests : créer une demande
func createRequest(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"general": "Les données envoyées sont invalides.",
		})
		return
	}

	// Contrôle de saisie : un message par champ en erreur
	if errs := cleanAndValidate(&req); len(errs) > 0 {
		writeErrors(w, http.StatusBadRequest, errs)
		return
	}

	req.Status = StatusOpen
	req.CreatedAt = time.Now()
	req.UpdatedAt = req.CreatedAt

	result, err := db.Exec(
		`INSERT INTO requests (title, description, requester, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		req.Title, req.Description, req.Requester, req.Status, req.CreatedAt, req.UpdatedAt,
	)
	if err != nil {
		writeErrors(w, http.StatusInternalServerError, map[string]string{
			"general": "Erreur du serveur : la demande n'a pas pu être enregistrée.",
		})
		return
	}
	id, _ := result.LastInsertId()
	req.ID = int(id)

	writeJSON(w, http.StatusCreated, req)
}
