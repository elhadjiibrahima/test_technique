package main

import (
	"database/sql"
	"os"
	"strings"

	_ "modernc.org/sqlite" // pilote SQLite (pur Go, sans compilateur C)
)

// Connexion à la base (partagée par toutes les fonctions)
var db *sql.DB

// dbPath renvoie le chemin du fichier de base.
// On peut le changer avec la variable d'environnement DB_PATH
// (utile sur un hébergeur pour mettre la base sur un disque persistant).
func dbPath() string {
	if p := os.Getenv("DB_PATH"); p != "" {
		return p
	}
	return "demandes.db"
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	name          TEXT NOT NULL,
	email         TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role          TEXT NOT NULL,
	created_at    TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS requests (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	title       TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	requester   TEXT NOT NULL,
	status      TEXT NOT NULL,
	created_at  TIMESTAMP NOT NULL,
	updated_at  TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS notifications (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	request_id INTEGER,
	message    TEXT NOT NULL,
	is_read    INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, is_read);

CREATE TABLE IF NOT EXISTS comments (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	request_id  INTEGER NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
	author_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
	author_name TEXT NOT NULL,
	author_role TEXT NOT NULL,
	message     TEXT NOT NULL,
	created_at  TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_comments_request ON comments(request_id);

CREATE TABLE IF NOT EXISTS request_events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	request_id INTEGER NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
	actor_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
	actor_name TEXT NOT NULL,
	message    TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_request ON request_events(request_id);

CREATE TABLE IF NOT EXISTS attachments (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	request_id   INTEGER NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
	uploader_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
	uploader     TEXT NOT NULL,
	filename     TEXT NOT NULL,
	content_type TEXT NOT NULL,
	size         INTEGER NOT NULL,
	data         BLOB NOT NULL,
	created_at   TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_attachments_request ON attachments(request_id);

CREATE TABLE IF NOT EXISTS password_resets (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at TIMESTAMP NOT NULL
);
`

// Colonnes ajoutées après la première version : on les ajoute aux bases
// existantes pour ne pas perdre les anciennes demandes.
var migrations = []string{
	`ALTER TABLE requests ADD COLUMN requester_id INTEGER REFERENCES users(id) ON DELETE SET NULL`,
	`ALTER TABLE requests ADD COLUMN assignee_id INTEGER REFERENCES users(id) ON DELETE SET NULL`,
	`ALTER TABLE requests ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal'`,
	`ALTER TABLE requests ADD COLUMN category TEXT NOT NULL DEFAULT 'autre'`,
	`ALTER TABLE users ADD COLUMN job_title TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN phone TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE users ADD COLUMN address TEXT NOT NULL DEFAULT ''`,
}

// initDB ouvre la base et crée les tables si elles n'existent pas.
// Pour les tests, on passe ":memory:" pour avoir une base vide en mémoire.
func initDB(path string) error {
	var err error
	db, err = sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1) // SQLite n'accepte qu'une écriture à la fois

	if _, err = db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	if _, err = db.Exec(schema); err != nil {
		return err
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}
