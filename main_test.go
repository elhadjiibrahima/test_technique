package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------- Outils de test ----------

// client simule un navigateur : il garde le cookie de session entre les requêtes
type client struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
}

func (c *client) do(method, url, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("X-Requested-With", "fetch")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == sessionCookie {
			c.cookie = ck
		}
	}
	return rec
}

// expect envoie la requête et vérifie le code HTTP
func (c *client) expect(code int, method, url, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	rec := c.do(method, url, body)
	if rec.Code != code {
		c.t.Errorf("%s %s : code %d, attendu %d (%s)", method, url, rec.Code, code, strings.TrimSpace(rec.Body.String()))
	}
	return rec
}

type env struct {
	h                                http.Handler
	admin, agent, agent2, alice, bob *client
	agentID, agent2ID, aliceID       int
}

// setup prépare une base vide en mémoire pour chaque test (les vraies données
// ne sont jamais touchées) et connecte un admin, deux agents et deux employés.
func setup(t *testing.T) *env {
	t.Helper()
	pbkdf2Iter = 1000 // les tests n'ont pas besoin d'un hachage lent
	limiter = &loginLimiter{failures: map[string][]time.Time{}}
	if err := initDB(":memory:"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	e := &env{h: routes()}
	mk := func(name, email string, role Role) (*client, int) {
		u, err := createUser(name, email, "motdepasse", role)
		if err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, h: e.h}
		c.expect(http.StatusOK, "POST", "/auth/login", `{"email":"`+email+`","password":"motdepasse"}`)
		return c, u.ID
	}
	e.admin, _ = mk("Awa Admin", "admin@test.gn", RoleAdmin)
	e.agent, e.agentID = mk("Moussa Agent", "agent@test.gn", RoleAgent)
	e.agent2, e.agent2ID = mk("Fanta Agent", "agent2@test.gn", RoleAgent)
	e.alice, e.aliceID = mk("Alice", "alice@test.gn", RoleEmploye)
	e.bob, _ = mk("Bob", "bob@test.gn", RoleEmploye)
	return e
}

// create crée une demande avec ce client et renvoie son numéro
func create(c *client, title string) int {
	c.t.Helper()
	rec := c.expect(http.StatusCreated, "POST", "/requests", `{"title":"`+title+`","description":"Pour le bureau"}`)
	var r Request
	json.NewDecoder(rec.Body).Decode(&r)
	return r.ID
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatalf("réponse illisible : %v", err)
	}
	return v
}

var itoa = strconv.Itoa

// ---------- Mots de passe et connexion ----------

func TestPassword(t *testing.T) {
	pbkdf2Iter = 1000
	h, err := hashPassword("secret123")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h, "secret123") {
		t.Error("le mot de passe ne doit pas apparaître dans l'empreinte")
	}
	if !checkPassword("secret123", h) || checkPassword("secret124", h) || checkPassword("secret123", "n'importe quoi") {
		t.Error("vérification du mot de passe incorrecte")
	}
	h2, _ := hashPassword("secret123")
	if h == h2 {
		t.Error("deux empreintes du même mot de passe doivent être différentes (sel aléatoire)")
	}
}

func TestRegisterLoginLogout(t *testing.T) {
	e := setup(t)
	c := &client{t: t, h: e.h}

	c.expect(http.StatusUnauthorized, "GET", "/requests", "")
	c.expect(http.StatusBadRequest, "POST", "/auth/register", `{"name":"Zoé","email":"pas-un-email","password":"court"}`)

	rec := c.expect(http.StatusCreated, "POST", "/auth/register", `{"name":"Zoé Martin","email":" Zoe@Test.GN ","password":"motdepasse"}`)
	u := decode[User](t, rec)
	if u.Role != RoleEmploye || u.Email != "zoe@test.gn" {
		t.Errorf("inscription : %+v (un nouveau compte doit être employé)", u)
	}
	// connecté directement après l'inscription
	c.expect(http.StatusOK, "GET", "/auth/me", "")

	// on ne peut pas s'inscrire en admin, ni deux fois avec le même e-mail
	c2 := &client{t: t, h: e.h}
	rec = c2.expect(http.StatusCreated, "POST", "/auth/register", `{"name":"Pirate","email":"p@test.gn","password":"motdepasse","role":"admin"}`)
	if decode[User](t, rec).Role != RoleEmploye {
		t.Error("le rôle envoyé à l'inscription doit être ignoré")
	}
	c2.expect(http.StatusConflict, "POST", "/auth/register", `{"name":"Zoé","email":"zoe@test.gn","password":"motdepasse"}`)

	c.expect(http.StatusNoContent, "POST", "/auth/logout", "")
	c.expect(http.StatusUnauthorized, "GET", "/auth/me", "")

	c.expect(http.StatusUnauthorized, "POST", "/auth/login", `{"email":"zoe@test.gn","password":"mauvais!!"}`)
	c.expect(http.StatusUnauthorized, "POST", "/auth/login", `{"email":"inconnu@test.gn","password":"motdepasse"}`)
	c.expect(http.StatusOK, "POST", "/auth/login", `{"email":"ZOE@test.gn","password":"motdepasse"}`)
}

func TestLoginBloqueApresTropDEchecs(t *testing.T) {
	e := setup(t)
	c := &client{t: t, h: e.h}
	for i := 0; i < maxLoginFailures; i++ {
		c.do("POST", "/auth/login", `{"email":"alice@test.gn","password":"mauvais!!"}`)
	}
	c.expect(http.StatusTooManyRequests, "POST", "/auth/login", `{"email":"alice@test.gn","password":"motdepasse"}`)
}

func TestCSRF(t *testing.T) {
	e := setup(t)
	req := httptest.NewRequest("POST", "/requests", strings.NewReader(`{"title":"Ecran"}`))
	req.AddCookie(e.alice.cookie) // le cookie est là, mais pas l'en-tête
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("requête sans X-Requested-With : code %d, attendu 403", rec.Code)
	}
}

func TestChangePassword(t *testing.T) {
	e := setup(t)
	e.alice.expect(http.StatusBadRequest, "PUT", "/auth/password", `{"current_password":"faux","new_password":"nouveaumdp"}`)
	e.alice.expect(http.StatusBadRequest, "PUT", "/auth/password", `{"current_password":"motdepasse","new_password":"court"}`)
	e.alice.expect(http.StatusNoContent, "PUT", "/auth/password", `{"current_password":"motdepasse","new_password":"nouveaumdp"}`)
	e.alice.expect(http.StatusOK, "GET", "/auth/me", "") // toujours connectée

	c := &client{t: t, h: e.h}
	c.expect(http.StatusUnauthorized, "POST", "/auth/login", `{"email":"alice@test.gn","password":"motdepasse"}`)
	c.expect(http.StatusOK, "POST", "/auth/login", `{"email":"alice@test.gn","password":"nouveaumdp"}`)
}

func TestEnsureAdmin(t *testing.T) {
	setup(t)
	t.Setenv("ADMIN_EMAIL", "Chef@Test.gn")
	t.Setenv("ADMIN_PASSWORD", "tresSecret1")
	t.Setenv("ADMIN_NAME", "Chef")
	if err := ensureAdmin(); err != nil {
		t.Fatal(err)
	}
	if err := ensureAdmin(); err != nil { // deuxième démarrage : pas de doublon
		t.Fatal(err)
	}
	var role Role
	db.QueryRow(`SELECT role FROM users WHERE email = 'chef@test.gn'`).Scan(&role)
	if role != RoleAdmin {
		t.Errorf("rôle %q, attendu admin", role)
	}
	t.Setenv("ADMIN_PASSWORD", "court")
	if ensureAdmin() == nil {
		t.Error("un mot de passe admin trop court doit être refusé")
	}
}

// ---------- Demandes et autorisations ----------

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{StatusOpen, StatusInProgress, StatusDone} {
		if !s.valid() {
			t.Errorf("%s devrait être valide", s)
		}
	}
	if Status("nimporte quoi").valid() {
		t.Error("un faux statut ne devrait pas être valide")
	}
}

func TestCreateRequest(t *testing.T) {
	e := setup(t)
	rec := e.alice.expect(http.StatusCreated, "POST", "/requests",
		`{"title":"  Nouvel écran  ","requester":"Quelqu'un d'autre","status":"done","description":"Pour le bureau"}`)
	r := decode[Request](t, rec)
	if r.Title != "Nouvel écran" || r.Requester != "Alice" || r.Status != StatusOpen ||
		r.RequesterID == nil || *r.RequesterID != e.aliceID {
		t.Errorf("demande inattendue : %+v", r)
	}
	e.alice.expect(http.StatusBadRequest, "POST", "/requests", `{"description":"sans titre"}`)
	e.alice.expect(http.StatusBadRequest, "POST", "/requests", `pas du json`)
}

func TestEmployeVoitSeulementSesDemandes(t *testing.T) {
	e := setup(t)
	a := create(e.alice, "Clavier")
	b := create(e.bob, "Souris")

	list := decode[[]Request](t, e.alice.expect(http.StatusOK, "GET", "/requests", ""))
	if len(list) != 1 || list[0].ID != a {
		t.Errorf("Alice devrait voir seulement sa demande : %+v", list)
	}
	stats := decode[map[string]int](t, e.alice.expect(http.StatusOK, "GET", "/requests/stats", ""))
	if stats["total"] != 1 {
		t.Errorf("stats d'Alice : %v", stats)
	}

	// Alice ne peut ni modifier ni supprimer la demande de Bob (404 : elle ne la voit pas)
	e.alice.expect(http.StatusNotFound, "PUT", "/requests/"+itoa(b), `{"title":"Pirate"}`)
	e.alice.expect(http.StatusNotFound, "DELETE", "/requests/"+itoa(b), "")

	// Les agents et les admins voient tout
	for _, c := range []*client{e.agent, e.admin} {
		if list := decode[[]Request](t, c.expect(http.StatusOK, "GET", "/requests", "")); len(list) != 2 {
			t.Errorf("un agent/admin devrait voir 2 demandes, pas %d", len(list))
		}
	}
}

func TestEmployeNePeutPasTraiter(t *testing.T) {
	e := setup(t)
	id := create(e.alice, "Clavier")
	e.alice.expect(http.StatusForbidden, "PATCH", "/requests/"+itoa(id), `{"status":"done"}`)
	e.alice.expect(http.StatusForbidden, "PATCH", "/requests/"+itoa(id)+"/assign", `{"assignee_id":`+itoa(e.aliceID)+`}`)
	e.alice.expect(http.StatusForbidden, "GET", "/users", "")
}

func TestEditEtSuppressionParLeDemandeur(t *testing.T) {
	e := setup(t)
	id := create(e.alice, "Clavier")

	rec := e.alice.expect(http.StatusOK, "PUT", "/requests/"+itoa(id), `{"title":"Clavier sans fil","description":"Azerty"}`)
	if r := decode[Request](t, rec); r.Title != "Clavier sans fil" || r.Requester != "Alice" {
		t.Errorf("modification : %+v", r)
	}
	e.alice.expect(http.StatusBadRequest, "PUT", "/requests/"+itoa(id), `{"title":""}`)

	// Une fois prise en charge, le demandeur ne peut plus modifier ni supprimer
	e.agent.expect(http.StatusOK, "PATCH", "/requests/"+itoa(id), `{"status":"in_progress"}`)
	e.alice.expect(http.StatusForbidden, "PUT", "/requests/"+itoa(id), `{"title":"Autre chose"}`)
	e.alice.expect(http.StatusForbidden, "DELETE", "/requests/"+itoa(id), "")

	// L'admin peut toujours
	e.admin.expect(http.StatusNoContent, "DELETE", "/requests/"+itoa(id), "")
	e.admin.expect(http.StatusNotFound, "DELETE", "/requests/"+itoa(id), "")

	// Assignée à un agent (même encore ouverte) : plus modifiable par le demandeur
	id3 := create(e.alice, "Lampe")
	e.admin.expect(http.StatusOK, "PATCH", "/requests/"+itoa(id3)+"/assign", `{"assignee_id":`+itoa(e.agentID)+`}`)
	e.alice.expect(http.StatusForbidden, "PUT", "/requests/"+itoa(id3), `{"title":"Autre lampe"}`)

	id2 := create(e.alice, "Souris")
	e.alice.expect(http.StatusNoContent, "DELETE", "/requests/"+itoa(id2), "")
	e.agent.expect(http.StatusForbidden, "DELETE", "/requests/"+itoa(create(e.bob, "Ecran")), "")
}

func TestTraitementParUnAgent(t *testing.T) {
	e := setup(t)
	id := create(e.alice, "Clavier")
	url := "/requests/" + itoa(id)

	e.agent.expect(http.StatusBadRequest, "PATCH", url, `{"status":"faux"}`)
	e.agent.expect(http.StatusBadRequest, "PATCH", "/requests/abc", `{"status":"done"}`)
	e.agent.expect(http.StatusNotFound, "PATCH", "/requests/999", `{"status":"done"}`)

	// Changer le statut d'une demande libre : l'agent la prend en charge
	rec := e.agent.expect(http.StatusOK, "PATCH", url, `{"status":"in_progress"}`)
	r := decode[Request](t, rec)
	if r.AssigneeID == nil || *r.AssigneeID != e.agentID || deref(r.AssigneeName) != "Moussa Agent" {
		t.Errorf("la demande devrait être assignée à l'agent : %+v", r)
	}

	// Un autre agent ne peut pas la traiter ni la prendre
	e.agent2.expect(http.StatusForbidden, "PATCH", url, `{"status":"done"}`)
	e.agent2.expect(http.StatusForbidden, "PATCH", url+"/assign", `{"assignee_id":`+itoa(e.agent2ID)+`}`)

	// Filtre "mes demandes"
	if list := decode[[]Request](t, e.agent.expect(http.StatusOK, "GET", "/requests?assignee=me", "")); len(list) != 1 {
		t.Errorf("assignee=me : %d demandes, attendu 1", len(list))
	}
	if list := decode[[]Request](t, e.agent2.expect(http.StatusOK, "GET", "/requests?assignee=me", "")); len(list) != 0 {
		t.Errorf("agent2 ne devrait avoir aucune demande")
	}

	// L'agent libère la demande, l'autre peut la prendre
	e.agent.expect(http.StatusOK, "PATCH", url+"/assign", `{"assignee_id":null}`)
	e.agent2.expect(http.StatusOK, "PATCH", url+"/assign", `{"assignee_id":`+itoa(e.agent2ID)+`}`)
	e.agent2.expect(http.StatusOK, "PATCH", url, `{"status":"done"}`)
}

func TestAssignationParLAdmin(t *testing.T) {
	e := setup(t)
	id := create(e.alice, "Clavier")
	url := "/requests/" + itoa(id) + "/assign"

	e.admin.expect(http.StatusBadRequest, "PATCH", url, `{"assignee_id":`+itoa(e.aliceID)+`}`) // pas un agent
	e.admin.expect(http.StatusBadRequest, "PATCH", url, `{"assignee_id":999}`)
	rec := e.admin.expect(http.StatusOK, "PATCH", url, `{"assignee_id":`+itoa(e.agentID)+`}`)
	if r := decode[Request](t, rec); r.AssigneeID == nil || *r.AssigneeID != e.agentID {
		t.Errorf("assignation : %+v", r)
	}
	// L'admin peut réassigner et changer le statut de n'importe quelle demande
	e.admin.expect(http.StatusOK, "PATCH", url, `{"assignee_id":`+itoa(e.agent2ID)+`}`)
	e.admin.expect(http.StatusOK, "PATCH", "/requests/"+itoa(id), `{"status":"done"}`)
}

func TestNotifications(t *testing.T) {
	e := setup(t)
	id := create(e.alice, "Clavier")

	type notifs struct {
		Unread int            `json:"unread"`
		Items  []Notification `json:"items"`
	}
	get := func(c *client) notifs {
		return decode[notifs](t, c.expect(http.StatusOK, "GET", "/notifications", ""))
	}

	// Nouvelle demande : les agents et admins sont prévenus, pas Bob
	if n := get(e.agent); n.Unread != 1 || !strings.Contains(n.Items[0].Message, "Nouvelle demande") {
		t.Errorf("agent : %+v", n)
	}
	if n := get(e.bob); n.Unread != 0 {
		t.Errorf("Bob ne devrait rien recevoir : %+v", n)
	}

	// Assignation : l'agent et la demandeuse sont prévenus
	e.admin.expect(http.StatusOK, "PATCH", "/requests/"+itoa(id)+"/assign", `{"assignee_id":`+itoa(e.agentID)+`}`)
	if n := get(e.agent); n.Unread != 2 || !strings.Contains(n.Items[0].Message, "vous a été assignée") {
		t.Errorf("agent après assignation : %+v", n)
	}

	// Changement de statut : Alice est prévenue
	e.agent.expect(http.StatusOK, "PATCH", "/requests/"+itoa(id), `{"status":"done"}`)
	n := get(e.alice)
	if n.Unread != 2 || !strings.Contains(n.Items[0].Message, "terminée") || n.Items[0].RequestID == nil {
		t.Errorf("Alice : %+v", n)
	}

	// Remettre le même statut ne renvoie pas de notification
	e.agent.expect(http.StatusOK, "PATCH", "/requests/"+itoa(id), `{"status":"done"}`)
	if get(e.alice).Unread != 2 {
		t.Error("pas de notification si le statut ne change pas")
	}

	e.alice.expect(http.StatusNoContent, "POST", "/notifications/read", "")
	if n := get(e.alice); n.Unread != 0 || len(n.Items) != 2 {
		t.Errorf("après lecture : %+v", n)
	}
}

func TestListPaginationEtFiltres(t *testing.T) {
	e := setup(t)
	for _, title := range []string{"Un", "Deux", "Trois", "Quatre", "Cinq"} {
		create(e.alice, title+" test")
	}

	rec := e.alice.expect(http.StatusOK, "GET", "/requests?page=2&limit=2", "")
	if got := rec.Header().Get("X-Total-Count"); got != "5" {
		t.Errorf("X-Total-Count = %q, attendu 5", got)
	}
	list := decode[[]Request](t, rec)
	if len(list) != 2 || list[0].Title != "Trois test" || list[1].Title != "Deux test" {
		t.Errorf("page 2 inattendue : %+v", list)
	}

	e.agent.expect(http.StatusOK, "PATCH", "/requests/"+itoa(list[0].ID), `{"status":"done"}`)
	rec = e.alice.expect(http.StatusOK, "GET", "/requests?status=done", "")
	if rec.Header().Get("X-Total-Count") != "1" {
		t.Error("filtre par statut incorrect")
	}
	if l := decode[[]Request](t, e.agent.expect(http.StatusOK, "GET", "/requests?assignee=none", "")); len(l) != 4 {
		t.Errorf("assignee=none : %d, attendu 4", len(l))
	}

	for _, url := range []string{"/requests?page=0", "/requests?limit=abc", "/requests?status=faux", "/requests?assignee=lui"} {
		e.alice.expect(http.StatusBadRequest, "GET", url, "")
	}
}

// ---------- Gestion des utilisateurs ----------

func TestGestionDesUtilisateurs(t *testing.T) {
	e := setup(t)
	e.agent.expect(http.StatusForbidden, "GET", "/users", "")

	rec := e.admin.expect(http.StatusOK, "GET", "/users", "")
	if strings.Contains(rec.Body.String(), "pbkdf2") {
		t.Fatal("l'empreinte du mot de passe ne doit jamais être envoyée")
	}
	if users := decode[[]User](t, rec); len(users) != 5 {
		t.Errorf("%d utilisateurs, attendu 5", len(users))
	}
	if agents := decode[[]User](t, e.admin.expect(http.StatusOK, "GET", "/users?role=agent", "")); len(agents) != 2 {
		t.Errorf("%d agents, attendu 2", len(agents))
	}

	// Promouvoir Alice agent : elle voit alors toutes les demandes
	create(e.bob, "Souris")
	e.admin.expect(http.StatusOK, "PATCH", "/users/"+itoa(e.aliceID), `{"role":"agent"}`)
	if l := decode[[]Request](t, e.alice.expect(http.StatusOK, "GET", "/requests", "")); len(l) != 1 {
		t.Error("Alice, devenue agent, devrait voir la demande de Bob")
	}
	e.admin.expect(http.StatusBadRequest, "PATCH", "/users/"+itoa(e.aliceID), `{"role":"roi"}`)

	// Un agent rétrogradé perd ses demandes assignées
	id := create(e.bob, "Ecran")
	e.agent.expect(http.StatusOK, "PATCH", "/requests/"+itoa(id), `{"status":"in_progress"}`)
	e.admin.expect(http.StatusOK, "PATCH", "/users/"+itoa(e.agentID), `{"role":"employe"}`)
	if r, _ := getRequest(id); r.AssigneeID != nil {
		t.Error("la demande devrait être libérée")
	}
	e.agent.expect(http.StatusForbidden, "PATCH", "/requests/"+itoa(id), `{"status":"done"}`)

	// Création d'un compte par l'admin
	e.admin.expect(http.StatusCreated, "POST", "/users", `{"name":"Nouvel Agent","email":"na@test.gn","password":"motdepasse","role":"agent"}`)
	e.admin.expect(http.StatusConflict, "POST", "/users", `{"name":"Doublon","email":"na@test.gn","password":"motdepasse","role":"agent"}`)

	// Suppression : la session de l'utilisateur ne marche plus
	e.admin.expect(http.StatusNoContent, "DELETE", "/users/"+itoa(e.agent2ID), "")
	e.agent2.expect(http.StatusUnauthorized, "GET", "/requests", "")
}

func TestAdminNePeutPasSeBloquer(t *testing.T) {
	e := setup(t)
	me := decode[User](t, e.admin.expect(http.StatusOK, "GET", "/auth/me", ""))
	e.admin.expect(http.StatusBadRequest, "PATCH", "/users/"+itoa(me.ID), `{"role":"employe"}`)
	e.admin.expect(http.StatusBadRequest, "DELETE", "/users/"+itoa(me.ID), "")
}

// ---------- Réponses sur les demandes ----------

func TestReponses(t *testing.T) {
	e := setup(t)
	id := create(e.alice, "Clavier")
	url := "/requests/" + itoa(id) + "/comments"

	type notifs struct {
		Unread int            `json:"unread"`
		Items  []Notification `json:"items"`
	}
	lastNotif := func(c *client) string {
		n := decode[notifs](t, c.expect(http.StatusOK, "GET", "/notifications", ""))
		if len(n.Items) == 0 {
			return ""
		}
		return n.Items[0].Message
	}

	// L'admin répond : Alice est notifiée
	rec := e.admin.expect(http.StatusCreated, "POST", url, `{"message":"  Bonjour, le clavier arrive lundi.  "}`)
	c := decode[Comment](t, rec)
	if c.Message != "Bonjour, le clavier arrive lundi." || c.AuthorName != "Awa Admin" || c.AuthorRole != RoleAdmin {
		t.Errorf("réponse inattendue : %+v", c)
	}
	if msg := lastNotif(e.alice); !strings.Contains(msg, "Awa Admin a répondu à votre demande") || !strings.Contains(msg, "arrive lundi") {
		t.Errorf("notification d'Alice : %q", msg)
	}

	// Alice répond : la demande n'est pas assignée, donc les agents et admins sont notifiés
	e.alice.expect(http.StatusCreated, "POST", url, `{"message":"Merci !"}`)
	if msg := lastNotif(e.agent); !strings.Contains(msg, "Alice a répondu") {
		t.Errorf("notification de l'agent : %q", msg)
	}

	// Une fois assignée, seul l'agent qui la traite est notifié
	e.admin.expect(http.StatusOK, "PATCH", "/requests/"+itoa(id)+"/assign", `{"assignee_id":`+itoa(e.agentID)+`}`)
	e.alice.expect(http.StatusCreated, "POST", url, `{"message":"Une question"}`)
	if msg := lastNotif(e.agent); !strings.Contains(msg, "Une question") {
		t.Errorf("l'agent assigné devrait être notifié : %q", msg)
	}
	if msg := lastNotif(e.agent2); strings.Contains(msg, "Une question") {
		t.Error("un autre agent ne devrait pas être notifié")
	}

	// Le fil, dans l'ordre, et le compteur sur la demande
	list := decode[[]Comment](t, e.alice.expect(http.StatusOK, "GET", url, ""))
	if len(list) != 3 || list[0].AuthorName != "Awa Admin" || list[2].Message != "Une question" {
		t.Errorf("fil inattendu : %+v", list)
	}
	if r, _ := getRequest(id); r.CommentCount != 3 {
		t.Errorf("comment_count = %d, attendu 3", r.CommentCount)
	}

	// Bob ne voit pas la demande d'Alice, ni ses réponses
	e.bob.expect(http.StatusNotFound, "GET", url, "")
	e.bob.expect(http.StatusNotFound, "POST", url, `{"message":"Pirate"}`)

	// Validation
	e.admin.expect(http.StatusBadRequest, "POST", url, `{"message":"   "}`)
	e.admin.expect(http.StatusBadRequest, "POST", url, `{"message":"`+strings.Repeat("a", commentMax+1)+`"}`)
	e.admin.expect(http.StatusNotFound, "POST", "/requests/999/comments", `{"message":"Bonjour"}`)

	// Supprimer la demande supprime ses réponses
	e.admin.expect(http.StatusNoContent, "DELETE", "/requests/"+itoa(id), "")
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM comments WHERE request_id = ?`, id).Scan(&n)
	if n != 0 {
		t.Errorf("%d réponses restantes après suppression", n)
	}
}

// ---------- Priorité et catégorie ----------

func TestPrioriteEtCategorie(t *testing.T) {
	e := setup(t)

	// Valeurs par défaut
	r := decode[Request](t, e.alice.expect(http.StatusCreated, "POST", "/requests", `{"title":"Souris"}`))
	if r.Priority != PriorityNormal || r.Category != "autre" {
		t.Errorf("valeurs par défaut : %s / %s", r.Priority, r.Category)
	}
	e.alice.expect(http.StatusBadRequest, "POST", "/requests", `{"title":"Ecran","priority":"tres urgent"}`)
	e.alice.expect(http.StatusBadRequest, "POST", "/requests", `{"title":"Ecran","category":"cuisine"}`)

	urgent := decode[Request](t, e.alice.expect(http.StatusCreated, "POST", "/requests",
		`{"title":"Serveur en panne","priority":"urgent","category":"informatique"}`))
	if urgent.Priority != PriorityUrgent || urgent.Category != "informatique" {
		t.Errorf("demande urgente : %+v", urgent)
	}
	create(e.alice, "Clavier") // normale, plus récente

	// Les agents sont prévenus que c'est urgent
	n := decode[struct {
		Items []Notification `json:"items"`
	}](t, e.agent.expect(http.StatusOK, "GET", "/notifications", ""))
	found := false
	for _, it := range n.Items {
		if strings.Contains(it.Message, "URGENT") && strings.Contains(it.Message, "Serveur en panne") {
			found = true
		}
	}
	if !found {
		t.Error("la notification d'une demande urgente devrait le dire")
	}

	// Tri par priorité : l'urgente d'abord, même si elle est plus ancienne
	list := decode[[]Request](t, e.agent.expect(http.StatusOK, "GET", "/requests?sort=priority", ""))
	if list[0].ID != urgent.ID {
		t.Errorf("tri par priorité : la première est %q", list[0].Title)
	}
	if list := decode[[]Request](t, e.agent.expect(http.StatusOK, "GET", "/requests", "")); list[0].Title != "Clavier" {
		t.Errorf("tri par défaut : la plus récente d'abord, pas %q", list[0].Title)
	}

	// Filtres
	if l := decode[[]Request](t, e.agent.expect(http.StatusOK, "GET", "/requests?priority=urgent", "")); len(l) != 1 {
		t.Errorf("filtre priorité : %d", len(l))
	}
	if l := decode[[]Request](t, e.agent.expect(http.StatusOK, "GET", "/requests?category=informatique", "")); len(l) != 1 {
		t.Errorf("filtre catégorie : %d", len(l))
	}
	for _, url := range []string{"/requests?priority=x", "/requests?category=x", "/requests?sort=x"} {
		e.agent.expect(http.StatusBadRequest, "GET", url, "")
	}

	// Le demandeur change la catégorie tant que personne n'a pris la demande
	r = decode[Request](t, e.alice.expect(http.StatusOK, "PUT", "/requests/"+itoa(r.ID),
		`{"title":"Souris","category":"materiel"}`))
	if r.Category != "materiel" || r.Priority != PriorityNormal {
		t.Errorf("modification : %+v (la priorité non envoyée doit rester)", r)
	}

	// Un agent change la priorité ; un employé ne peut pas
	e.alice.expect(http.StatusForbidden, "PATCH", "/requests/"+itoa(r.ID), `{"priority":"urgent"}`)
	r = decode[Request](t, e.agent.expect(http.StatusOK, "PATCH", "/requests/"+itoa(r.ID), `{"priority":"low"}`))
	if r.Priority != PriorityLow || r.Status != StatusOpen {
		t.Errorf("changement de priorité : %+v", r)
	}
	e.agent.expect(http.StatusBadRequest, "PATCH", "/requests/"+itoa(r.ID), `{"priority":"x"}`)
	e.agent.expect(http.StatusBadRequest, "PATCH", "/requests/"+itoa(r.ID), `{}`)
}

// ---------- Historique ----------

func TestHistorique(t *testing.T) {
	e := setup(t)
	id := create(e.alice, "Clavier")
	url := "/requests/" + itoa(id)

	e.alice.expect(http.StatusOK, "PUT", url, `{"title":"Clavier sans fil","description":"Pour le bureau","priority":"urgent"}`)
	e.alice.expect(http.StatusOK, "PUT", url, `{"title":"Clavier sans fil","description":"Pour le bureau","priority":"urgent"}`) // rien ne change
	e.admin.expect(http.StatusOK, "PATCH", url+"/assign", `{"assignee_id":`+itoa(e.agentID)+`}`)
	e.admin.expect(http.StatusOK, "PATCH", url+"/assign", `{"assignee_id":`+itoa(e.agentID)+`}`) // déjà assignée
	e.agent.expect(http.StatusOK, "PATCH", url, `{"status":"in_progress"}`)
	e.agent.expect(http.StatusOK, "PATCH", url+"/assign", `{"assignee_id":null}`)
	e.agent.expect(http.StatusOK, "PATCH", url, `{"status":"done","priority":"normal"}`)

	events := decode[[]Event](t, e.alice.expect(http.StatusOK, "GET", url+"/history", ""))
	want := []string{
		"Alice:a créé la demande",
		"Alice:a modifié la demande (titre : « Clavier » → « Clavier sans fil », priorité : normale → urgente)",
		"Awa Admin:a assigné la demande à Moussa Agent",
		"Moussa Agent:a changé le statut : ouverte → en cours",
		"Moussa Agent:a libéré la demande",
		"Moussa Agent:a pris la demande en charge",
		"Moussa Agent:a changé le statut : en cours → terminée",
		"Moussa Agent:a changé la priorité : urgente → normale",
	}
	if len(events) != len(want) {
		for _, ev := range events {
			t.Log(ev.ActorName, ":", ev.Message)
		}
		t.Fatalf("%d lignes d'historique, attendu %d", len(events), len(want))
	}
	for i, w := range want {
		got := events[i].ActorName + ":" + events[i].Message
		if !strings.HasPrefix(got, w) {
			t.Errorf("ligne %d : %q, attendu %q", i, got, w)
		}
	}

	e.bob.expect(http.StatusNotFound, "GET", url+"/history", "")
}

// ---------- Pièces jointes ----------

// Un petit PNG valide (1 pixel)
var pngData = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

func (c *client) upload(code int, url, filename string, data []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filename)
	fw.Write(data)
	mw.Close()

	req := httptest.NewRequest("POST", url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "fetch")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	if rec.Code != code {
		c.t.Errorf("upload %s : code %d, attendu %d (%s)", filename, rec.Code, code, strings.TrimSpace(rec.Body.String()))
	}
	return rec
}

func TestPiecesJointes(t *testing.T) {
	e := setup(t)
	id := create(e.alice, "Ecran casse")
	url := "/requests/" + itoa(id) + "/attachments"

	a := decode[Attachment](t, e.alice.upload(http.StatusCreated, url, `../../photo "écran".jpeg`, pngData))
	if a.ContentType != "image/png" || a.Filename != "photo écran.png" || a.Size != len(pngData) {
		t.Errorf("pièce jointe : %+v (le type et l'extension viennent du contenu)", a)
	}

	// Refusés : faux type, fichier vide, trop gros
	e.alice.upload(http.StatusBadRequest, url, "virus.png", []byte("MZ\x90\x00 ceci est un exe"))
	e.alice.upload(http.StatusBadRequest, url, "page.html", []byte("<html><script>alert(1)</script></html>"))
	e.alice.upload(http.StatusBadRequest, url, "vide.png", nil)
	big := append(append([]byte{}, pngData...), make([]byte, maxFileSize)...)
	e.alice.upload(http.StatusBadRequest, url, "gros.png", big)

	// Le demandeur est notifié quand l'équipe ajoute un fichier
	e.admin.upload(http.StatusCreated, url, "devis.pdf", []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF"))
	if n := decode[struct {
		Items []Notification `json:"items"`
	}](t, e.alice.expect(http.StatusOK, "GET", "/notifications", "")); !strings.Contains(n.Items[0].Message, "pièce jointe") {
		t.Errorf("notification : %+v", n.Items)
	}

	// Liste et compteur
	list := decode[[]Attachment](t, e.agent.expect(http.StatusOK, "GET", url, ""))
	if len(list) != 2 {
		t.Fatalf("%d pièces jointes, attendu 2", len(list))
	}
	if r, _ := getRequest(id); r.FileCount != 2 {
		t.Errorf("attachment_count = %d", r.FileCount)
	}

	// Téléchargement : image affichée, PDF téléchargé, en-têtes de sécurité
	rec := e.alice.expect(http.StatusOK, "GET", "/attachments/"+itoa(a.ID), "")
	if !bytes.Equal(rec.Body.Bytes(), pngData) || rec.Header().Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline") ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("téléchargement de l'image : %v", rec.Header())
	}
	rec = e.alice.expect(http.StatusOK, "GET", "/attachments/"+itoa(list[1].ID), "")
	if !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Error("un PDF doit être téléchargé, pas affiché")
	}

	// Bob ne voit ni la liste ni les fichiers, et ne peut rien ajouter
	e.bob.expect(http.StatusNotFound, "GET", url, "")
	e.bob.expect(http.StatusNotFound, "GET", "/attachments/"+itoa(a.ID), "")
	e.bob.upload(http.StatusNotFound, url, "x.png", pngData)

	// Suppression : celui qui l'a ajoutée ou un admin
	e.agent.expect(http.StatusForbidden, "DELETE", "/attachments/"+itoa(a.ID), "")
	e.alice.expect(http.StatusNoContent, "DELETE", "/attachments/"+itoa(a.ID), "")
	e.alice.expect(http.StatusForbidden, "DELETE", "/attachments/"+itoa(list[1].ID), "")
	e.admin.expect(http.StatusNoContent, "DELETE", "/attachments/"+itoa(list[1].ID), "")
	e.admin.expect(http.StatusNotFound, "GET", "/attachments/"+itoa(a.ID), "")

	// 5 fichiers maximum par demande
	for i := 0; i < maxFilesPerRequest; i++ {
		e.alice.upload(http.StatusCreated, url, "p.png", pngData)
	}
	e.alice.upload(http.StatusBadRequest, url, "trop.png", pngData)

	// Historique
	events := decode[[]Event](t, e.alice.expect(http.StatusOK, "GET", "/requests/"+itoa(id)+"/history", ""))
	joined := ""
	for _, ev := range events {
		joined += ev.Message + "\n"
	}
	if !strings.Contains(joined, "a ajouté la pièce jointe « photo écran.png »") ||
		!strings.Contains(joined, "a supprimé la pièce jointe « devis.pdf »") {
		t.Errorf("historique :\n%s", joined)
	}

	// Supprimer la demande supprime ses fichiers
	e.admin.expect(http.StatusNoContent, "DELETE", "/requests/"+itoa(id), "")
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM attachments`).Scan(&n)
	if n != 0 {
		t.Errorf("%d fichiers restants", n)
	}
}

// ---------- Mot de passe oublié ----------

func TestMotDePasseOublie(t *testing.T) {
	e := setup(t)
	anon := &client{t: t, h: e.h}

	// Même réponse que le compte existe ou non ; les admins sont prévenus
	anon.expect(http.StatusNoContent, "POST", "/auth/forgot", `{"email":"alice@test.gn"}`)
	anon.expect(http.StatusNoContent, "POST", "/auth/forgot", `{"email":"inconnu@test.gn"}`)
	n := decode[struct {
		Items []Notification `json:"items"`
	}](t, e.admin.expect(http.StatusOK, "GET", "/notifications", ""))
	if len(n.Items) != 1 || !strings.Contains(n.Items[0].Message, "Alice (alice@test.gn) a oublié son mot de passe") {
		t.Errorf("notification admin : %+v", n.Items)
	}

	// Seul un admin génère le lien
	e.agent.expect(http.StatusForbidden, "POST", "/users/"+itoa(e.aliceID)+"/reset-link", "")
	e.admin.expect(http.StatusNotFound, "POST", "/users/999/reset-link", "")
	link := decode[map[string]any](t, e.admin.expect(http.StatusCreated, "POST", "/users/"+itoa(e.aliceID)+"/reset-link", ""))
	path, _ := link["path"].(string)
	token := strings.TrimPrefix(path, "/?reset=")
	if !strings.HasPrefix(path, "/?reset=") || len(token) < 40 {
		t.Fatalf("lien inattendu : %v", link)
	}

	// Un nouveau lien annule le précédent
	link2 := decode[map[string]any](t, e.admin.expect(http.StatusCreated, "POST", "/users/"+itoa(e.aliceID)+"/reset-link", ""))
	anon.expect(http.StatusBadRequest, "POST", "/auth/reset", `{"token":"`+token+`","password":"nouveaumdp"}`)
	token = strings.TrimPrefix(link2["path"].(string), "/?reset=")

	anon.expect(http.StatusBadRequest, "POST", "/auth/reset", `{"token":"faux","password":"nouveaumdp"}`)
	anon.expect(http.StatusBadRequest, "POST", "/auth/reset", `{"token":"`+token+`","password":"court"}`)
	anon.expect(http.StatusNoContent, "POST", "/auth/reset", `{"token":"`+token+`","password":"nouveaumdp"}`)

	// Le lien ne sert qu'une fois, les anciennes sessions sont fermées, le nouveau mot de passe marche
	anon.expect(http.StatusBadRequest, "POST", "/auth/reset", `{"token":"`+token+`","password":"autremdp1"}`)
	e.alice.expect(http.StatusUnauthorized, "GET", "/auth/me", "")
	anon.expect(http.StatusUnauthorized, "POST", "/auth/login", `{"email":"alice@test.gn","password":"motdepasse"}`)
	anon.expect(http.StatusOK, "POST", "/auth/login", `{"email":"alice@test.gn","password":"nouveaumdp"}`)

	// Lien expiré
	link3 := decode[map[string]any](t, e.admin.expect(http.StatusCreated, "POST", "/users/"+itoa(e.aliceID)+"/reset-link", ""))
	db.Exec(`UPDATE password_resets SET expires_at = ?`, time.Now().Add(-time.Minute))
	anon.expect(http.StatusBadRequest, "POST", "/auth/reset",
		`{"token":"`+strings.TrimPrefix(link3["path"].(string), "/?reset=")+`","password":"nouveaumdp2"}`)
}
