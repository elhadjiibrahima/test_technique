package main

import "time"

type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

// valid vérifie que le statut est l'une des 3 valeurs autorisées
func (s Status) valid() bool {
	return s == StatusOpen || s == StatusInProgress || s == StatusDone
}

type Request struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Requester   string    `json:"requester"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
