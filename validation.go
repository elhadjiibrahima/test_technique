package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limites de taille des champs (les mêmes que dans la page web)
const (
	titleMin       = 3
	titleMax       = 100
	requesterMin   = 2
	requesterMax   = 50
	descriptionMax = 500
)

// cleanAndValidate enlève les espaces inutiles autour des textes,
// puis vérifie chaque champ.
// Elle renvoie une map : nom du champ -> message d'erreur.
// Si la map est vide, tout est correct.
func cleanAndValidate(req *Request) map[string]string {
	req.Title = strings.TrimSpace(req.Title)
	req.Requester = strings.TrimSpace(req.Requester)
	req.Description = strings.TrimSpace(req.Description)

	errs := map[string]string{}

	// utf8.RuneCountInString compte les vrais caractères (un "é" compte pour 1)
	titleLen := utf8.RuneCountInString(req.Title)
	requesterLen := utf8.RuneCountInString(req.Requester)
	descriptionLen := utf8.RuneCountInString(req.Description)

	switch {
	case !onlyLetters(req.Title):
		errs["title"] = "Le titre ne doit contenir que des lettres (espaces, tirets et apostrophes autorisés)."
	case titleLen == 0:
		errs["title"] = "Le titre est obligatoire."
	case titleLen < titleMin:
		errs["title"] = "Le titre est trop court (3 caractères minimum)."
	case titleLen > titleMax:
		errs["title"] = "Le titre est trop long (100 caractères maximum)."
	}

	switch {
	case requesterLen == 0:
		errs["requester"] = "Votre nom est obligatoire."
	case requesterLen < requesterMin:
		errs["requester"] = "Le nom est trop court (2 caractères minimum)."
	case requesterLen > requesterMax:
		errs["requester"] = "Le nom est trop long (50 caractères maximum)."
	case !onlyLetters(req.Requester):
		errs["requester"] = "Le nom ne doit contenir que des lettres (espaces, tirets et apostrophes autorisés)."
	}

	if descriptionLen > descriptionMax {
		errs["description"] = "La description est trop longue (500 caractères maximum)."
	}

	return errs
}

// onlyLetters vérifie que le texte ne contient que des lettres
// (avec accents), des espaces, des tirets ou des apostrophes.
// Exemples acceptés : "Alice", "Jean-Pierre", "Zoé Martin", "O'Brien"
func onlyLetters(text string) bool {
	for _, c := range text {
		if !unicode.IsLetter(c) && c != ' ' && c != '-' && c != '\'' {
			return false
		}
	}
	return true
}
