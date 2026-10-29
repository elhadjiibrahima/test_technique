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
