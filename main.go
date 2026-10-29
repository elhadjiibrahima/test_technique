package main

import "net/http"



func main() {
	initDB()
	defer db.Close()

	http.HandleFunc("GET /{$}", homePage)
	http.HandleFunc("POST /requests", createRequest)
	http.HandleFunc("GET /requests", listRequests)
	http.HandleFunc("PATCH /requests/{id}", updateStatus)

	println("Serveur sur http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}
