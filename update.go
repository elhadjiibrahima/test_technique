package main

import (
	"fmt"
	"net/http"
	"time"
)

// PATCH /requests/{id} : changer le statut d'une demande (agents et admins).
// Un agent ne peut traiter que les demandes libres ou qui lui sont assignées ;
// s'il change le statut d'une demande libre, elle lui est assignée.
func updateStatus(w http.ResponseWriter, r *http.Request, u User) {
	current, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}

	var body struct {
		Status Status `json:"status"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Status.valid() {
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"general": "Statut invalide : choisissez ouverte, en cours ou terminée.",
		})
		return
	}

	assignee := current.AssigneeID
	if u.Role == RoleAgent {
		if assignee != nil && *assignee != u.ID {
			writeErrors(w, http.StatusForbidden, map[string]string{
				"general": "Cette demande est traitée par " + deref(current.AssigneeName) + ".",
			})
			return
		}
		assignee = &u.ID // l'agent prend la demande en charge
	}

	if _, err := db.Exec(
		`UPDATE requests SET status = ?, assignee_id = ?, updated_at = ? WHERE id = ?`,
		body.Status, assignee, time.Now(), current.ID,
	); err != nil {
		writeErrors(w, http.StatusInternalServerError, map[string]string{
			"general": "Erreur du serveur : le statut n'a pas pu être modifié.",
		})
		return
	}

	if body.Status != current.Status {
		notifyUnlessActor(current.RequesterID, u, &current.ID, fmt.Sprintf(
			"Votre demande #%d « %s » est maintenant %s (par %s).",
			current.ID, current.Title, statusLabels[body.Status], u.Name))
	}

	updated, err := getRequest(current.ID)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// PATCH /requests/{id}/assign : choisir qui traite la demande.
// Corps : {"assignee_id": 3} pour assigner, {"assignee_id": null} pour libérer.
//   - un admin peut assigner n'importe quel agent ou admin, ou libérer
//   - un agent peut seulement prendre une demande libre, ou libérer la sienne
func assignRequest(w http.ResponseWriter, r *http.Request, u User) {
	current, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}

	var body struct {
		AssigneeID *int `json:"assignee_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if u.Role == RoleAgent {
		taking := body.AssigneeID != nil && *body.AssigneeID == u.ID && current.AssigneeID == nil
		releasing := body.AssigneeID == nil && current.AssigneeID != nil && *current.AssigneeID == u.ID
		if !taking && !releasing {
			forbidden(w)
			return
		}
	}

	if body.AssigneeID != nil {
		ok, err := isAgent(*body.AssigneeID)
		if err != nil {
			serverError(w)
			return
		}
		if !ok {
			writeErrors(w, http.StatusBadRequest, map[string]string{
				"general": "On ne peut assigner une demande qu'à un agent ou un administrateur.",
			})
			return
		}
	}

	if _, err := db.Exec(`UPDATE requests SET assignee_id = ?, updated_at = ? WHERE id = ?`,
		body.AssigneeID, time.Now(), current.ID); err != nil {
		serverError(w)
		return
	}

	updated, err := getRequest(current.ID)
	if err != nil {
		serverError(w)
		return
	}

	if body.AssigneeID != nil {
		notifyUnlessActor(body.AssigneeID, u, &current.ID, fmt.Sprintf(
			"La demande #%d « %s » vous a été assignée par %s.", current.ID, current.Title, u.Name))
		notifyUnlessActor(current.RequesterID, u, &current.ID, fmt.Sprintf(
			"Votre demande #%d « %s » est prise en charge par %s.", current.ID, current.Title, deref(updated.AssigneeName)))
	}

	writeJSON(w, http.StatusOK, updated)
}

func deref(s *string) string {
	if s == nil {
		return "un autre agent"
	}
	return *s
}
