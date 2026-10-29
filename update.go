package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// PATCH /requests/{id} : changer le statut d'une demande
func updateStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id invalide", http.StatusBadRequest)
		return
	}

	var body struct {
		Status Status `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "JSON invalide", http.StatusBadRequest)
		return
	}
	if !body.Status.valid() {
		http.Error(w, "status doit être open, in_progress ou done", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(
		`UPDATE requests SET status = ?, updated_at = ? WHERE id = ?`,
		body.Status, time.Now(), id,
	)
	if err != nil {
		http.Error(w, "erreur base de données", http.StatusInternalServerError)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		http.Error(w, "demande introuvable", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": body.Status})
}
