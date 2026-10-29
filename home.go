package main

import "net/http"

// GET / : afficher la page web
func homePage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}
