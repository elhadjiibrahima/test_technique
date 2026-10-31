package main

import (
	"fmt"
	"net/http"
	"time"
)

// PATCH /requests/{id} : changer le statut et/ou la priorité d'une demande (agents et admins).
// Corps : {"status": "in_progress"}, {"priority": "urgent"} ou les deux.
// Un agent ne peut traiter que les demandes libres ou qui lui sont assignées ;
// s'il change une demande libre, elle lui est assignée.
func updateStatus(w http.ResponseWriter, r *http.Request, u User) {
	current, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}

	var body struct {
		Status   *Status   `json:"status"`
		Priority *Priority `json:"priority"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Status == nil && body.Priority == nil {
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"general": "Indiquez un statut ou une priorité.",
		})
		return
	}
	newStatus, newPriority := current.Status, current.Priority
	if body.Status != nil {
		if !body.Status.valid() {
			writeErrors(w, http.StatusBadRequest, map[string]string{
				"general": "Statut invalide : choisissez ouverte, en cours ou terminée.",
			})
			return
		}
		newStatus = *body.Status
	}
	if body.Priority != nil {
		if !body.Priority.valid() {
			writeErrors(w, http.StatusBadRequest, map[string]string{
				"general": "Priorité invalide : basse, normale ou urgente.",
			})
			return
		}
		newPriority = *body.Priority
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
		`UPDATE requests SET status = ?, priority = ?, assignee_id = ?, updated_at = ? WHERE id = ?`,
		newStatus, newPriority, assignee, time.Now(), current.ID,
	); err != nil {
		writeErrors(w, http.StatusInternalServerError, map[string]string{
			"general": "Erreur du serveur : la demande n'a pas pu être modifiée.",
		})
		return
	}

	if current.AssigneeID == nil && assignee != nil {
		logEvent(current.ID, u, "a pris la demande en charge")
	}
	if newStatus != current.Status {
		logEvent(current.ID, u, fmt.Sprintf("a changé le statut : %s → %s",
			statusLabels[current.Status], statusLabels[newStatus]))
		notifyUnlessActor(current.RequesterID, u, &current.ID, fmt.Sprintf(
			"Votre demande #%d « %s » est maintenant %s (par %s).",
			current.ID, current.Title, statusLabels[newStatus], u.Name))
	}
	if newPriority != current.Priority {
		logEvent(current.ID, u, fmt.Sprintf("a changé la priorité : %s → %s",
			priorityLabels[current.Priority], priorityLabels[newPriority]))
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

	// Rien ne change : pas d'historique ni de notification en double
	if (body.AssigneeID == nil && current.AssigneeID == nil) ||
		(body.AssigneeID != nil && current.AssigneeID != nil && *body.AssigneeID == *current.AssigneeID) {
		writeJSON(w, http.StatusOK, current)
		return
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

	switch {
	case body.AssigneeID == nil && current.AssigneeID != nil:
		logEvent(current.ID, u, "a libéré la demande (plus personne ne la traite)")
	case body.AssigneeID != nil && *body.AssigneeID == u.ID:
		logEvent(current.ID, u, "a pris la demande en charge")
	case body.AssigneeID != nil:
		logEvent(current.ID, u, "a assigné la demande à "+deref(updated.AssigneeName))
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
