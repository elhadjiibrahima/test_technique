package main

import (
	"encoding/json"
	"net/http"
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
