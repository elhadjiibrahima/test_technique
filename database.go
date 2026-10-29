package main

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite" // pilote SQLite (pur Go, sans compilateur C)
)

// Connexion à la base (partagée par toutes les fonctions)
var db *sql.DB

// initDB ouvre le fichier demandes.db et crée la table si elle n'existe pas
func initDB() {
	var err error
	db, err = sql.Open("sqlite", "demandes.db")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1) // SQLite n'accepte qu'une écriture à la fois

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS requests (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		title       TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		requester   TEXT NOT NULL,
		status      TEXT NOT NULL,
		created_at  TIMESTAMP NOT NULL,
		updated_at  TIMESTAMP NOT NULL
	)`)
	if err != nil {
		log.Fatal(err)
	}
}
