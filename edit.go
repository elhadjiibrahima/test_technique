package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const requestColumns = `r.id, r.title, r.description, r.requester, r.requester_id,
	r.assignee_id, a.name, r.status, r.created_at, r.updated_at`

const requestFrom = ` FROM requests r LEFT JOIN users a ON a.id = r.assignee_id`

// scanRequest lit une ligne produite avec requestColumns
func scanRequest(row interface{ Scan(...any) error }) (Request, error) {
	var req Request
	err := row.Scan(&req.ID, &req.Title, &req.Description, &req.Requester, &req.RequesterID,
		&req.AssigneeID, &req.AssigneeName, &req.Status, &req.CreatedAt, &req.UpdatedAt)
	return req, err
}

// getRequest lit une demande par son numéro
func getRequest(id int) (Request, error) {
	return scanRequest(db.QueryRow(`SELECT `+requestColumns+requestFrom+` WHERE r.id = ?`, id))
}

// isOwner : la demande a-t-elle été créée par cet utilisateur ?
func isOwner(u User, req Request) bool {
	return req.RequesterID != nil && *req.RequesterID == u.ID
}

// canSee : un employé ne voit que ses demandes, agents et admins voient tout
func canSee(u User, req Request) bool {
	return u.Role.canProcess() || isOwner(u, req)
}

// canModify : modifier ou supprimer une demande.
// L'admin peut toujours ; le demandeur seulement tant que personne ne l'a
// prise en charge (statut "ouverte" et aucun agent assigné).
func canModify(u User, req Request) bool {
	return u.Role == RoleAdmin || (isOwner(u, req) && req.Status == StatusOpen && req.AssigneeID == nil)
}

// loadVisibleRequest lit la demande de l'URL et vérifie que l'utilisateur peut la voir.
// Si ce n'est pas le cas, on répond 404 (sans dire que la demande existe).
func loadVisibleRequest(w http.ResponseWriter, r *http.Request, u User) (Request, bool) {
	id, ok := parseID(w, r)
	if !ok {
		return Request{}, false
	}
	req, err := getRequest(id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !canSee(u, req)) {
		notFound(w)
		return Request{}, false
	}
	if err != nil {
		serverError(w)
		return Request{}, false
	}
	return req, true
}

// PUT /requests/{id} : modifier le titre et la description d'une demande.
// Le statut ne change pas ici (on utilise PATCH pour ça).
func editRequest(w http.ResponseWriter, r *http.Request, u User) {
	current, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}
	if !canModify(u, current) {
		writeErrors(w, http.StatusForbidden, map[string]string{
			"general": "Vous ne pouvez plus modifier cette demande : elle est déjà prise en charge.",
		})
		return
	}

	var req Request
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Requester = current.Requester // le demandeur ne change pas

	// Même contrôle de saisie que pour la création
	if errs := cleanAndValidate(&req); len(errs) > 0 {
		writeErrors(w, http.StatusBadRequest, errs)
		return
	}

	if _, err := db.Exec(
		`UPDATE requests SET title = ?, description = ?, updated_at = ? WHERE id = ?`,
		req.Title, req.Description, time.Now(), current.ID,
	); err != nil {
		serverError(w)
		return
	}

	notifyUnlessActor(current.RequesterID, u, &current.ID,
		fmt.Sprintf("Votre demande #%d a été modifiée par %s.", current.ID, u.Name))

	// On renvoie la demande complète, telle qu'elle est maintenant en base
	updated, err := getRequest(current.ID)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// DELETE /requests/{id} : supprimer une demande
func deleteRequest(w http.ResponseWriter, r *http.Request, u User) {
	current, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}
	if !canModify(u, current) {
		writeErrors(w, http.StatusForbidden, map[string]string{
			"general": "Vous ne pouvez plus supprimer cette demande : elle est déjà prise en charge.",
		})
		return
	}

	if _, err := db.Exec(`DELETE FROM requests WHERE id = ?`, current.ID); err != nil {
		serverError(w)
		return
	}
	db.Exec(`UPDATE notifications SET request_id = NULL WHERE request_id = ?`, current.ID)

	notifyUnlessActor(current.RequesterID, u, nil,
		fmt.Sprintf("Votre demande #%d « %s » a été supprimée par %s.", current.ID, current.Title, u.Name))

	w.WriteHeader(http.StatusNoContent) // 204 : supprimé, rien à renvoyer
}
