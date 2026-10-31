package main

import (
	"encoding/json"
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
