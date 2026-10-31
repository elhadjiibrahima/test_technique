package main

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GET /requests/export.csv : télécharger les demandes visibles au format CSV
// (ouvrable dans Excel). Accepte les mêmes filtres que GET /requests.
// Un employé n'exporte que ses propres demandes.
func exportRequests(w http.ResponseWriter, r *http.Request, u User) {
	where, args, order, errMsg := requestFilters(u, r.URL.Query())
	if errMsg != "" {
		writeErrors(w, http.StatusBadRequest, map[string]string{"general": errMsg})
		return
	}
	rows, err := db.Query(`SELECT `+requestColumns+requestFrom+whereSQL(where)+order, args...)
	if err != nil {
		serverError(w)
		return
	}
	defer rows.Close()

	var list []Request
	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			serverError(w)
			return
		}
		list = append(list, req)
	}
	rows.Close()

	filename := "demandes-" + time.Now().Format("2006-01-02") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Write([]byte("\xEF\xBB\xBF")) // pour qu'Excel lise bien les accents

	out := csv.NewWriter(w)
	out.Comma = ';' // séparateur attendu par Excel en français
	out.Write([]string{"N°", "Titre", "Description", "Catégorie", "Priorité", "Statut",
		"Demandeur", "Poste du demandeur", "Traitée par", "Créée le", "Terminée le",
		"Délai (heures)", "Note", "Commentaire de la note", "Réponses", "Pièces jointes"})

	const format = "02/01/2006 15:04"
	for _, req := range list {
		resolved, delay, rating := "", "", ""
		if req.ResolvedAt != nil {
			resolved = req.ResolvedAt.Local().Format(format)
			// virgule décimale, comme dans Excel en français
			delay = strings.Replace(strconv.FormatFloat(req.ResolvedAt.Sub(req.CreatedAt).Hours(), 'f', 1, 64), ".", ",", 1)
		}
		if req.Rating != nil {
			rating = strconv.Itoa(*req.Rating) + "/5"
		}
		assignee := ""
		if req.AssigneeName != nil {
			assignee = *req.AssigneeName
		}
		job := ""
		if req.RequesterJob != nil {
			job = *req.RequesterJob
		}
		out.Write(safeCells(
			strconv.Itoa(req.ID), req.Title, req.Description, categoryLabels[req.Category],
			priorityLabels[req.Priority], statusLabels[req.Status], req.Requester, job, assignee,
			req.CreatedAt.Local().Format(format), resolved, delay, rating, req.RatingNote,
			strconv.Itoa(req.CommentCount), strconv.Itoa(req.FileCount),
		))
	}
	out.Flush()
}

// safeCells empêche l'injection de formules : une cellule qui commence par
// = + - @ serait exécutée comme une formule par Excel. On la préfixe d'une apostrophe.
func safeCells(cells ...string) []string {
	for i, c := range cells {
		if c != "" && strings.ContainsRune("=+-@\t\r", rune(c[0])) {
			cells[i] = "'" + c
		}
	}
	return cells
}
