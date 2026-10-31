package main

import (
	"log"
	"net/http"
	"os"
)

// routes déclare toutes les routes (séparé de main pour pouvoir les tester).
// requireAuth(...) : il faut être connecté ; les rôles listés limitent l'accès.
func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", homePage)

	// Comptes et connexion
	mux.HandleFunc("POST /auth/register", register)
	mux.HandleFunc("POST /auth/login", login)
	mux.HandleFunc("POST /auth/logout", logout)
	mux.HandleFunc("GET /auth/me", requireAuth(me))
	mux.HandleFunc("PUT /auth/password", requireAuth(changePassword))
	mux.HandleFunc("PUT /auth/profile", requireAuth(updateProfile))
	mux.HandleFunc("POST /auth/forgot", forgotPassword)
	mux.HandleFunc("POST /auth/reset", resetPassword)

	// Demandes
	mux.HandleFunc("POST /requests", requireAuth(createRequest))
	mux.HandleFunc("GET /requests", requireAuth(listRequests))
	mux.HandleFunc("GET /requests/stats", requireAuth(requestStats))
	mux.HandleFunc("PUT /requests/{id}", requireAuth(editRequest))
	mux.HandleFunc("DELETE /requests/{id}", requireAuth(deleteRequest))
	mux.HandleFunc("PATCH /requests/{id}", requireAuth(updateStatus, RoleAgent, RoleAdmin))
	mux.HandleFunc("PATCH /requests/{id}/assign", requireAuth(assignRequest, RoleAgent, RoleAdmin))

	// Réponses sur une demande (admin, agents, et le demandeur)
	mux.HandleFunc("GET /requests/{id}/comments", requireAuth(listComments))
	mux.HandleFunc("POST /requests/{id}/comments", requireAuth(addComment))

	// Historique et pièces jointes
	mux.HandleFunc("GET /requests/{id}/history", requireAuth(listHistory))
	mux.HandleFunc("GET /requests/{id}/attachments", requireAuth(listAttachments))
	mux.HandleFunc("POST /requests/{id}/attachments", requireAuth(uploadAttachment))
	mux.HandleFunc("GET /attachments/{id}", requireAuth(downloadAttachment))
	mux.HandleFunc("DELETE /attachments/{id}", requireAuth(deleteAttachment))

	// Notifications
	mux.HandleFunc("GET /notifications", requireAuth(listNotifications))
	mux.HandleFunc("POST /notifications/read", requireAuth(markNotificationsRead))

	// Gestion des utilisateurs (admin)
	mux.HandleFunc("GET /users", requireAuth(listUsers, RoleAdmin))
	mux.HandleFunc("POST /users", requireAuth(adminCreateUser, RoleAdmin))
	mux.HandleFunc("PATCH /users/{id}", requireAuth(updateUserRole, RoleAdmin))
	mux.HandleFunc("DELETE /users/{id}", requireAuth(deleteUser, RoleAdmin))
	mux.HandleFunc("POST /users/{id}/reset-link", requireAuth(createResetLink, RoleAdmin))

	return securityHeaders(csrfGuard(mux))
}

// securityHeaders ajoute quelques en-têtes de sécurité à toutes les réponses
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func main() {
	if err := initDB(dbPath()); err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := ensureAdmin(); err != nil {
		log.Fatal(err)
	}

	// Les hébergeurs comme Render donnent le port à utiliser dans PORT
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Serveur sur http://localhost:" + port)
	log.Fatal(http.ListenAndServe(":"+port, routes()))
}
