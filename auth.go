package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	sessionCookie   = "session"
	sessionDuration = 7 * 24 * time.Hour
	passwordMin     = 8
	passwordMax     = 128
)

// ---------- Mots de passe ----------

// Nombre d'itérations PBKDF2-SHA512 (recommandation OWASP : 210 000).
// C'est une variable pour que les tests puissent aller plus vite.
var pbkdf2Iter = 210_000

// hashPassword renvoie "pbkdf2-sha512$iterations$sel$empreinte".
// On ne stocke jamais le mot de passe lui-même.
func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha512.New, password, salt, pbkdf2Iter, 64)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha512$%d$%s$%s", pbkdf2Iter, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// checkPassword compare un mot de passe avec l'empreinte enregistrée
func checkPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha512" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha512.New, password, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// Empreinte factice : on fait le même calcul quand l'e-mail n'existe pas,
// pour ne pas révéler par le temps de réponse quels comptes existent.
var dummyHash, _ = hashPassword("mot-de-passe-factice")

// ---------- Sessions ----------

// hashToken : en base, on garde l'empreinte du jeton, pas le jeton lui-même
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// startSession crée une session et envoie le cookie au navigateur
func startSession(w http.ResponseWriter, r *http.Request, userID int) error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(sessionDuration)

	// On en profite pour supprimer les sessions expirées
	db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now())

	if _, err := db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), userID, expires); err != nil {
		return err
	}
	setSessionCookie(w, r, token, expires)
	return nil
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true, // inaccessible au JavaScript
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
}

// currentUser renvoie l'utilisateur connecté (ok = false si personne)
func currentUser(r *http.Request) (User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return User{}, false
	}
	var u User
	err = db.QueryRow(
		`SELECT u.id, u.name, u.email, u.role, u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = ? AND s.expires_at > ?`,
		hashToken(c.Value), time.Now(),
	).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt)
	return u, err == nil
}

// ---------- Contrôle d'accès ----------

type authedHandler func(http.ResponseWriter, *http.Request, User)

// requireAuth : la route demande d'être connecté.
// Si des rôles sont donnés, seul l'un de ces rôles peut y accéder.
func requireAuth(h authedHandler, roles ...Role) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := currentUser(r)
		if !ok {
			writeErrors(w, http.StatusUnauthorized, map[string]string{
				"general": "Vous devez être connecté.",
			})
			return
		}
		if len(roles) > 0 && !hasRole(u, roles) {
			forbidden(w)
			return
		}
		h(w, r, u)
	}
}

func hasRole(u User, roles []Role) bool {
	for _, role := range roles {
		if u.Role == role {
			return true
		}
	}
	return false
}

func forbidden(w http.ResponseWriter) {
	writeErrors(w, http.StatusForbidden, map[string]string{
		"general": "Vous n'avez pas le droit de faire cette action.",
	})
}

// csrfGuard bloque les requêtes qui modifient des données si elles ne viennent
// pas de notre page. Un autre site ne peut pas ajouter l'en-tête
// X-Requested-With sans l'accord du navigateur (CORS), donc il ne peut pas
// utiliser le cookie de session à la place de l'utilisateur.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("X-Requested-With") != "fetch" {
				forbidden(w)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- Limite des tentatives de connexion ----------

// Après 5 échecs pour un même e-mail, on bloque 15 minutes.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

var limiter = &loginLimiter{failures: map[string][]time.Time{}}

const (
	maxLoginFailures = 5
	loginWindow      = 15 * time.Minute
)

func (l *loginLimiter) recent(key string) []time.Time {
	var kept []time.Time
	for _, t := range l.failures[key] {
		if time.Since(t) < loginWindow {
			kept = append(kept, t)
		}
	}
	l.failures[key] = kept
	return kept
}

func (l *loginLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) >= maxLoginFailures
}

func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[key] = append(l.recent(key), time.Now())
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

// ---------- Validation ----------

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateName(name string) string {
	n := utf8.RuneCountInString(name)
	switch {
	case n == 0:
		return "Le nom est obligatoire."
	case n < requesterMin:
		return "Le nom est trop court (2 caractères minimum)."
	case n > requesterMax:
		return "Le nom est trop long (50 caractères maximum)."
	case !onlyLetters(name):
		return "Le nom ne doit contenir que des lettres (espaces, tirets et apostrophes autorisés)."
	}
	return ""
}

func validateEmail(email string) string {
	if email == "" {
		return "L'e-mail est obligatoire."
	}
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || !strings.Contains(email[strings.Index(email, "@"):], ".") || len(email) > 254 {
		return "L'e-mail n'est pas valide."
	}
	return ""
}

func validatePassword(password string) string {
	n := utf8.RuneCountInString(password)
	switch {
	case n < passwordMin:
		return "Le mot de passe doit faire au moins 8 caractères."
	case n > passwordMax:
		return "Le mot de passe est trop long (128 caractères maximum)."
	}
	return ""
}

// ---------- Utilisateurs ----------

var errEmailTaken = errors.New("email déjà utilisé")

// createUser enregistre un nouvel utilisateur
func createUser(name, email, password string, role Role) (User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	u := User{Name: name, Email: email, Role: role, CreatedAt: time.Now()}
	res, err := db.Exec(`INSERT INTO users (name, email, password_hash, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		u.Name, u.Email, hash, u.Role, u.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, errEmailTaken
		}
		return User{}, err
	}
	id, _ := res.LastInsertId()
	u.ID = int(id)
	return u, nil
}

// ensureAdmin crée (ou remet à jour) le compte administrateur défini par
// ADMIN_EMAIL, ADMIN_PASSWORD et ADMIN_NAME. Comme ça, il y a toujours un
// admin, même si la base repart de zéro (par exemple après un redéploiement).
func ensureAdmin() error {
	email := normalizeEmail(os.Getenv("ADMIN_EMAIL"))
	password := os.Getenv("ADMIN_PASSWORD")
	if email == "" || password == "" {
		return nil
	}
	name := strings.TrimSpace(os.Getenv("ADMIN_NAME"))
	if validateName(name) != "" {
		name = "Administrateur"
	}
	if msg := validateEmail(email); msg != "" {
		return errors.New("ADMIN_EMAIL : " + msg)
	}
	if msg := validatePassword(password); msg != "" {
		return errors.New("ADMIN_PASSWORD : " + msg)
	}

	var id int
	var stored string
	err := db.QueryRow(`SELECT id, password_hash FROM users WHERE email = ?`, email).Scan(&id, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = createUser(name, email, password, RoleAdmin)
		if err == nil {
			log.Println("Compte administrateur créé :", email)
		}
		return err
	}
	if err != nil {
		return err
	}
	// Le compte existe : on s'assure qu'il est admin. Son mot de passe actuel
	// est gardé (il a pu être changé depuis la page).
	_, err = db.Exec(`UPDATE users SET role = ? WHERE id = ?`, RoleAdmin, id)
	return err
}

// ---------- Routes /auth ----------

type credentials struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"general": "Les données envoyées sont invalides.",
		})
		return false
	}
	return true
}

// POST /auth/register : créer un compte (toujours avec le rôle employé)
func register(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decodeJSON(w, r, &c) {
		return
	}
	c.Name = strings.TrimSpace(c.Name)
	c.Email = normalizeEmail(c.Email)

	errs := map[string]string{}
	if msg := validateName(c.Name); msg != "" {
		errs["name"] = msg
	}
	if msg := validateEmail(c.Email); msg != "" {
		errs["email"] = msg
	}
	if msg := validatePassword(c.Password); msg != "" {
		errs["password"] = msg
	}
	if len(errs) > 0 {
		writeErrors(w, http.StatusBadRequest, errs)
		return
	}

	u, err := createUser(c.Name, c.Email, c.Password, RoleEmploye)
	if errors.Is(err, errEmailTaken) {
		writeErrors(w, http.StatusConflict, map[string]string{
			"email": "Un compte existe déjà avec cet e-mail.",
		})
		return
	}
	if err != nil || startSession(w, r, u.ID) != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

// POST /auth/login : se connecter
func login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !decodeJSON(w, r, &c) {
		return
	}
	email := normalizeEmail(c.Email)

	if limiter.blocked(email) {
		writeErrors(w, http.StatusTooManyRequests, map[string]string{
			"general": "Trop de tentatives. Réessayez dans 15 minutes.",
		})
		return
	}

	var u User
	var stored string
	err := db.QueryRow(`SELECT id, name, email, role, created_at, password_hash FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &stored)
	if err != nil {
		checkPassword(c.Password, dummyHash) // même durée que si le compte existait
	}
	if err != nil || !checkPassword(c.Password, stored) {
		limiter.fail(email)
		writeErrors(w, http.StatusUnauthorized, map[string]string{
			"general": "E-mail ou mot de passe incorrect.",
		})
		return
	}

	limiter.reset(email)
	if startSession(w, r, u.ID) != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// POST /auth/logout : se déconnecter
func logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(c.Value))
	}
	setSessionCookie(w, r, "", time.Unix(0, 0))
	w.WriteHeader(http.StatusNoContent)
}

// GET /auth/me : qui est connecté ?
func me(w http.ResponseWriter, r *http.Request, u User) {
	writeJSON(w, http.StatusOK, u)
}

// PUT /auth/password : changer son mot de passe
func changePassword(w http.ResponseWriter, r *http.Request, u User) {
	var body struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	var stored string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, u.ID).Scan(&stored); err != nil {
		serverError(w)
		return
	}
	if !checkPassword(body.Current, stored) {
		writeErrors(w, http.StatusBadRequest, map[string]string{
			"current_password": "Le mot de passe actuel est incorrect.",
		})
		return
	}
	if msg := validatePassword(body.New); msg != "" {
		writeErrors(w, http.StatusBadRequest, map[string]string{"new_password": msg})
		return
	}

	hash, err := hashPassword(body.New)
	if err != nil {
		serverError(w)
		return
	}
	if _, err := db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, u.ID); err != nil {
		serverError(w)
		return
	}
	// On déconnecte les autres appareils, et on garde cette session-ci
	db.Exec(`DELETE FROM sessions WHERE user_id = ?`, u.ID)
	if startSession(w, r, u.ID) != nil {
		serverError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
