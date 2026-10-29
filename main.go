package main

import "net/http"

// routes déclare toutes les routes (séparé de main pour pouvoir les tester)
func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", homePage)
	mux.HandleFunc("POST /requests", createRequest)
	mux.HandleFunc("GET /requests", listRequests)
	mux.HandleFunc("PATCH /requests/{id}", updateStatus)
	return mux
}

func main() {
	initDB()
	defer db.Close()

	println("Serveur sur http://localhost:8080")
	http.ListenAndServe(":8080", routes())
}
