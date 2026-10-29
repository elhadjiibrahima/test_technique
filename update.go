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
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"general": "Numéro de demande invalide.",
		})
		return
	}

	var body struct {
		Status Status `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"general": "Les données envoyées sont invalides.",
		})
		return
	}
	if !body.Status.valid() {
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"general": "Statut invalide : choisissez ouverte, en cours ou terminée.",
		})
		return
	}

	result, err := db.Exec(
		`UPDATE requests SET status = ?, updated_at = ? WHERE id = ?`,
		body.Status, time.Now(), id,
	)
	if err != nil {
		writeErrors(w, http.StatusInternalServerError, map[string]string{
			"general": "Erreur du serveur : le statut n'a pas pu être modifié.",
		})
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		writeErrors(w, http.StatusNotFound, map[string]string{
			"general": "Cette demande n'existe pas (elle a peut-être été supprimée).",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": body.Status})
}
