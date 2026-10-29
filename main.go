package main

import (
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	initDB()
	defer db.Close()

	http.HandleFunc("GET /{$}", homePage)
	http.HandleFunc("POST /requests", createRequest)
	http.HandleFunc("GET /requests", listRequests)
	http.HandleFunc("PATCH /requests/{id}", updateStatus)

	println("Serveur sur le port " + port)
	http.ListenAndServe(":"+port, nil)
}
