package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

// GET /users : liste des utilisateurs (admin)
// GET /users?role=agent : seulement les agents (pour l'assignation)
func listUsers(w http.ResponseWriter, r *http.Request, _ User) {
	query := `SELECT id, name, email, role, created_at FROM users`
	args := []any{}
	if role := Role(r.URL.Query().Get("role")); role != "" {
		if !role.valid() {
			writeErrors(w, http.StatusBadRequest, map[string]string{"general": "Rôle invalide."})
			return
		}
		query += ` WHERE role = ?`
		args = append(args, role)
	}
	rows, err := db.Query(query+` ORDER BY name`, args...)
	if err != nil {
		serverError(w)
		return
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt); err != nil {
			serverError(w)
			return
		}
		users = append(users, u)
	}
	writeJSON(w, http.StatusOK, users)
}

// POST /users : un admin crée un compte avec le rôle de son choix
func adminCreateUser(w http.ResponseWriter, r *http.Request, _ User) {
	var body struct {
		credentials
		Role Role `json:"role"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Email = normalizeEmail(body.Email)

	errs := map[string]string{}
	if msg := validateName(body.Name); msg != "" {
		errs["name"] = msg
	}
	if msg := validateEmail(body.Email); msg != "" {
		errs["email"] = msg
	}
	if msg := validatePassword(body.Password); msg != "" {
		errs["password"] = msg
	}
	if !body.Role.valid() {
		errs["role"] = "Rôle invalide : employe, agent ou admin."
	}
	if len(errs) > 0 {
		writeErrors(w, http.StatusBadRequest, errs)
		return
	}

	u, err := createUser(body.Name, body.Email, body.Password, body.Role)
	if errors.Is(err, errEmailTaken) {
		writeErrors(w, http.StatusConflict, map[string]string{"email": "Un compte existe déjà avec cet e-mail."})
		return
	}
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

// PATCH /users/{id} : changer le rôle d'un utilisateur (admin)
func updateUserRole(w http.ResponseWriter, r *http.Request, admin User) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body struct {
		Role Role `json:"role"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !body.Role.valid() {
		writeErrors(w, http.StatusBadRequest, map[string]string{"role": "Rôle invalide : employe, agent ou admin."})
		return
	}
	if id == admin.ID {
		// Évite qu'un admin se retire ses propres droits par erreur
		writeErrors(w, http.StatusBadRequest, map[string]string{"general": "Vous ne pouvez pas changer votre propre rôle."})
		return
	}

	res, err := db.Exec(`UPDATE users SET role = ? WHERE id = ?`, body.Role, id)
	if err != nil {
		serverError(w)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		userNotFound(w)
		return
	}

	// Un utilisateur qui n'est plus agent ne garde pas les demandes qu'il traitait
	if !body.Role.canProcess() {
		db.Exec(`UPDATE requests SET assignee_id = NULL WHERE assignee_id = ?`, id)
	}
	notify(id, nil, "Votre rôle a été changé : vous êtes maintenant "+roleLabels[body.Role]+".")

	u, err := getUser(id)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// DELETE /users/{id} : supprimer un compte (admin)
// Ses demandes sont gardées (le nom du demandeur reste affiché).
func deleteUser(w http.ResponseWriter, r *http.Request, admin User) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if id == admin.ID {
		writeErrors(w, http.StatusBadRequest, map[string]string{"general": "Vous ne pouvez pas supprimer votre propre compte."})
		return
	}
	res, err := db.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		serverError(w)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		userNotFound(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var roleLabels = map[Role]string{
	RoleEmploye: "employé",
	RoleAgent:   "agent",
	RoleAdmin:   "administrateur",
}

func getUser(id int) (User, error) {
	var u User
	err := db.QueryRow(`SELECT id, name, email, role, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt)
	return u, err
}

func userNotFound(w http.ResponseWriter) {
	writeErrors(w, http.StatusNotFound, map[string]string{"general": "Cet utilisateur n'existe pas."})
}

// isAgent vérifie qu'un utilisateur existe et peut traiter des demandes
func isAgent(id int) (bool, error) {
	u, err := getUser(id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && u.Role.canProcess(), err
}
