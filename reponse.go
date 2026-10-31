package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// writeJSON écrit une réponse JSON avec le code HTTP donné
func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

// writeErrors renvoie les erreurs sous la forme :
// {"errors": {"title": "Le titre est obligatoire."}}
func writeErrors(w http.ResponseWriter, code int, errs map[string]string) {
	writeJSON(w, code, map[string]any{"errors": errs})
}

// serverError renvoie une erreur 500 avec un message lisible
func serverError(w http.ResponseWriter) {
	writeErrors(w, http.StatusInternalServerError, map[string]string{
		"general": "Erreur du serveur, réessayez plus tard.",
	})
}

// parseID lit le numéro de demande dans l'URL (/requests/{id}).
// En cas d'erreur, la réponse 400 est déjà envoyée et ok vaut false.
func parseID(w http.ResponseWriter, r *http.Request) (id int, ok bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"general": "Numéro de demande invalide.",
		})
		return 0, false
	}
	return id, true
}

// notFound renvoie une erreur 404 pour une demande qui n'existe pas
func notFound(w http.ResponseWriter) {
	writeErrors(w, http.StatusNotFound, map[string]string{
		"general": "Cette demande n'existe pas (elle a peut-être été supprimée).",
	})
}
