package main

import (
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultLimit = 10
	maxLimit     = 100
)

// visibleScope renvoie la condition SQL des demandes que l'utilisateur peut voir :
// un employé ne voit que ses propres demandes.
func visibleScope(u User) (where []string, args []any) {
	if !u.Role.canProcess() {
		return []string{"r.requester_id = ?"}, []any{u.ID}
	}
	return nil, nil
}

func whereSQL(where []string) string {
	if len(where) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(where, " AND ")
}

// GET /requests : lister les demandes visibles (les plus récentes en premier)
//
// Pagination (optionnelle) : ?page=2&limit=10
// Filtres (optionnels)     : ?status=open  ?assignee=me  ?assignee=none
//
// Le corps de la réponse reste une liste JSON. Le nombre total de demandes
// (avec les filtres) est envoyé dans l'en-tête X-Total-Count.
// Sans "page" ni "limit", toutes les demandes sont renvoyées.
func listRequests(w http.ResponseWriter, r *http.Request, u User) {
	q := r.URL.Query()
	where, args := visibleScope(u)

	if s := q.Get("status"); s != "" {
		if !Status(s).valid() {
			writeErrors(w, http.StatusBadRequest, map[string]string{
				"general": "Statut invalide : choisissez ouverte, en cours ou terminée.",
			})
			return
		}
		where, args = append(where, "r.status = ?"), append(args, s)
	}

	switch q.Get("assignee") {
	case "":
	case "me":
		where, args = append(where, "r.assignee_id = ?"), append(args, u.ID)
	case "none":
		where = append(where, "r.assignee_id IS NULL")
	default:
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"general": "Le filtre assignee doit valoir me ou none.",
		})
		return
	}

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM requests r`+whereSQL(where), args...).Scan(&total); err != nil {
		serverError(w)
		return
	}

	query := `SELECT ` + requestColumns + requestFrom + whereSQL(where) + ` ORDER BY r.id DESC`

	if q.Has("page") || q.Has("limit") {
		page, okPage := positiveInt(q.Get("page"), 1)
		limit, okLimit := positiveInt(q.Get("limit"), defaultLimit)
		if !okPage || !okLimit {
			writeErrors(w, http.StatusBadRequest, map[string]string{
				"general": "Les paramètres page et limit doivent être des nombres positifs.",
			})
			return
		}
		limit = min(limit, maxLimit)
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, (page-1)*limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		serverError(w)
		return
	}
	defer rows.Close()

	list := []Request{} // slice vide (et pas nil) pour renvoyer [] en JSON
	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			serverError(w)
			return
		}
		list = append(list, req)
	}
	if rows.Err() != nil {
		serverError(w)
		return
	}

	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, http.StatusOK, list)
}

// GET /requests/stats : nombre de demandes visibles par statut
// Réponse : {"open": 2, "in_progress": 1, "done": 3, "total": 6, "mine": 1}
// "mine" = demandes assignées à l'utilisateur et pas encore terminées.
func requestStats(w http.ResponseWriter, r *http.Request, u User) {
	stats := map[Status]int{StatusOpen: 0, StatusInProgress: 0, StatusDone: 0}
	where, args := visibleScope(u)

	rows, err := db.Query(`SELECT r.status, COUNT(*) FROM requests r`+whereSQL(where)+` GROUP BY r.status`, args...)
	if err != nil {
		serverError(w)
		return
	}
	total := 0
	for rows.Next() {
		var s Status
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			rows.Close()
			serverError(w)
			return
		}
		stats[s] = n
		total += n
	}
	rows.Close()

	var mine int
	if err := db.QueryRow(`SELECT COUNT(*) FROM requests WHERE assignee_id = ? AND status != ?`,
		u.ID, StatusDone).Scan(&mine); err != nil {
		serverError(w)
		return
	}

	writeJSON(w, http.StatusOK, map[string]int{
		"open":        stats[StatusOpen],
		"in_progress": stats[StatusInProgress],
		"done":        stats[StatusDone],
		"total":       total,
		"mine":        mine,
	})
}

// positiveInt lit un entier > 0. Si le texte est vide, renvoie la valeur par défaut.
func positiveInt(text string, def int) (int, bool) {
	if text == "" {
		return def, true
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
