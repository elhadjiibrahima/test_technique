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
		http.Error(w, "JSON invalide", http.StatusBadRequest)
		return
	}
	if req.Title == "" || req.Requester == "" {
		http.Error(w, "title et requester obligatoires", http.StatusBadRequest)
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
		http.Error(w, "erreur base de données", http.StatusInternalServerError)
		return
	}
	id, _ := result.LastInsertId()
	req.ID = int(id)

	writeJSON(w, http.StatusCreated, req)
}
