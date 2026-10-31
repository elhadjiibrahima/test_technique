package main

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	jobTitleMax = 80
	addressMax  = 200
)

// cleanText enlève les espaces en trop (y compris les retours à la ligne)
func cleanText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// validateJobTitle : le poste est obligatoire, ex : « Comptable », « Chef de projet IT »
func validateJobTitle(job string) string {
	n := utf8.RuneCountInString(job)
	switch {
	case n == 0:
		return "Le poste est obligatoire."
	case n < 2:
		return "Le poste est trop court (2 caractères minimum)."
	case n > jobTitleMax:
		return "Le poste est trop long (80 caractères maximum)."
	}
	for _, r := range job {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" -'’/&.,()", r) {
			return "Le poste contient un caractère non autorisé."
		}
	}
	return ""
}

// validatePhone : optionnel ; chiffres, espaces, +, -, points et parenthèses,
// entre 6 et 15 chiffres (ex : +224 620 00 00 00)
func validatePhone(phone string) string {
	if phone == "" {
		return ""
	}
	digits := 0
	for i, r := range phone {
		switch {
		case unicode.IsDigit(r) && r < 128:
			digits++
		case r == '+' && i == 0, r == ' ', r == '-', r == '.', r == '(', r == ')':
		default:
			return "Le numéro de téléphone ne doit contenir que des chiffres, des espaces et « + » au début."
		}
	}
	if digits < 6 || digits > 15 {
		return "Le numéro de téléphone doit avoir entre 6 et 15 chiffres."
	}
	return ""
}

// validateAddress : optionnelle, 200 caractères maximum
func validateAddress(address string) string {
	if utf8.RuneCountInString(address) > addressMax {
		return "L'adresse est trop longue (200 caractères maximum)."
	}
	return ""
}

// PUT /auth/profile : l'utilisateur modifie son profil
// Corps : {"name": "...", "job_title": "...", "phone": "...", "address": "..."}
// L'e-mail et le rôle ne se changent pas ici.
func updateProfile(w http.ResponseWriter, r *http.Request, u User) {
	var body struct {
		Name     string `json:"name"`
		JobTitle string `json:"job_title"`
		Phone    string `json:"phone"`
		Address  string `json:"address"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.JobTitle = cleanText(body.JobTitle)
	body.Phone = cleanText(body.Phone)
	body.Address = strings.TrimSpace(body.Address)

	errs := map[string]string{}
	if msg := validateName(body.Name); msg != "" {
		errs["name"] = msg
	}
	if msg := validateJobTitle(body.JobTitle); msg != "" {
		errs["job_title"] = msg
	}
	if msg := validatePhone(body.Phone); msg != "" {
		errs["phone"] = msg
	}
	if msg := validateAddress(body.Address); msg != "" {
		errs["address"] = msg
	}
	if len(errs) > 0 {
		writeErrors(w, http.StatusBadRequest, errs)
		return
	}

	if _, err := db.Exec(`UPDATE users SET name = ?, job_title = ?, phone = ?, address = ? WHERE id = ?`,
		body.Name, body.JobTitle, body.Phone, body.Address, u.ID); err != nil {
		serverError(w)
		return
	}
	// Le nouveau nom apparaît aussi sur ses demandes
	if body.Name != u.Name {
		db.Exec(`UPDATE requests SET requester = ? WHERE requester_id = ?`, body.Name, u.ID)
	}

	liveUsersChanged()
	updated, err := getUser(u.ID)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
