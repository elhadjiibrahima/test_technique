package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"time"
)

// Mot de passe oublié, sans envoi d'e-mail :
//  1. l'utilisateur clique « Mot de passe oublié ? » et donne son e-mail
//     → les admins reçoivent une notification
//  2. un admin génère un lien de réinitialisation (valable 24 h, une seule fois)
//     et le transmet à l'utilisateur (WhatsApp, SMS, en personne…)
//  3. l'utilisateur ouvre le lien et choisit un nouveau mot de passe
//
// Quand un service d'e-mail sera branché, l'étape 2 pourra envoyer le lien automatiquement.

const resetDuration = 24 * time.Hour

// POST /auth/forgot : demander une réinitialisation.
// La réponse est toujours la même, que le compte existe ou non : on ne révèle
// pas quels e-mails ont un compte.
func forgotPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	email := normalizeEmail(body.Email)
	key := "oubli:" + email

	if validateEmail(email) == "" && !limiter.blocked(key) {
		limiter.fail(key) // au plus 5 demandes par e-mail et par 15 minutes
		if u, err := getUserByEmail(email); err == nil && u.Active {
			notifyAdmins(u.ID, u.Name+" ("+u.Email+") a oublié son mot de passe. "+
				"Allez dans Utilisateurs → 🔑 pour générer un lien de réinitialisation.")
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /users/{id}/reset-link : un admin génère un lien de réinitialisation.
// Réponse : {"path": "/?reset=…", "expires_at": "…"} — la page ajoute l'adresse du site.
func createResetLink(w http.ResponseWriter, r *http.Request, admin User) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	target, err := getUser(id)
	if errors.Is(err, sql.ErrNoRows) {
		userNotFound(w)
		return
	}
	if err != nil {
		serverError(w)
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		serverError(w)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(resetDuration)

	// Un seul lien valable à la fois par utilisateur
	db.Exec(`DELETE FROM password_resets WHERE user_id = ? OR expires_at < ?`, target.ID, time.Now())
	if _, err := db.Exec(`INSERT INTO password_resets (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), target.ID, expires); err != nil {
		serverError(w)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"path":       "/?reset=" + token,
		"expires_at": expires,
		"user":       target,
	})
}

// POST /auth/reset : choisir un nouveau mot de passe avec le lien reçu
func resetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	invalid := func() {
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"general": "Ce lien n'est plus valable. Demandez-en un nouveau à un administrateur.",
		})
	}

	var userID int
	err := db.QueryRow(`SELECT user_id FROM password_resets WHERE token_hash = ? AND expires_at > ?`,
		hashToken(body.Token), time.Now()).Scan(&userID)
	if err != nil {
		invalid()
		return
	}
	if msg := validatePassword(body.Password); msg != "" {
		writeErrors(w, http.StatusBadRequest, map[string]string{"password": msg})
		return
	}

	hash, err := hashPassword(body.Password)
	if err != nil {
		serverError(w)
		return
	}
	// Le lien ne sert qu'une fois, et toutes les anciennes sessions sont fermées
	res, err := db.Exec(`DELETE FROM password_resets WHERE token_hash = ?`, hashToken(body.Token))
	if n, _ := res.RowsAffected(); err != nil || n == 0 {
		invalid()
		return
	}
	db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
	db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)

	if u, err := getUser(userID); err == nil {
		limiter.reset(u.Email)
		notify(u.ID, nil, "Votre mot de passe a été réinitialisé.")
	}
	w.WriteHeader(http.StatusNoContent)
}

func getUserByEmail(email string) (User, error) {
	return scanUser(db.QueryRow(`SELECT `+userColumns+` FROM users WHERE email = ?`, email))
}

// notifyAdmins prévient tous les administrateurs
func notifyAdmins(exceptID int, message string) {
	rows, err := db.Query(`SELECT id FROM users WHERE role = ? AND id != ? AND active = 1`, RoleAdmin, exceptID)
	if err != nil {
		return
	}
	var ids []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		notify(id, nil, message)
	}
}
