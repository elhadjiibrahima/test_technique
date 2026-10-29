package main

import "net/http"

// GET /requests : lister les demandes
func listRequests(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(
		`SELECT id, title, description, requester, status, created_at, updated_at
		 FROM requests ORDER BY id`)
	if err != nil {
		http.Error(w, "erreur base de données", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	list := []Request{} // slice vide (et pas nil) pour renvoyer [] en JSON
	for rows.Next() {
		var req Request
		err := rows.Scan(&req.ID, &req.Title, &req.Description, &req.Requester,
			&req.Status, &req.CreatedAt, &req.UpdatedAt)
		if err != nil {
			http.Error(w, "erreur base de données", http.StatusInternalServerError)
			return
		}
		list = append(list, req)
	}
	writeJSON(w, http.StatusOK, list)
}
