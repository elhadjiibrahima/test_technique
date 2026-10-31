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

// Libellés en français, utilisés dans les notifications
var statusLabels = map[Status]string{
	StatusOpen:       "ouverte",
	StatusInProgress: "en cours",
	StatusDone:       "terminée",
}

// Role : ce qu'un utilisateur a le droit de faire
//   - employe : crée ses demandes et ne voit que les siennes
//   - agent   : traite les demandes (prise en charge, changement de statut)
//   - admin   : tout, plus la gestion des utilisateurs et l'assignation
type Role string

const (
	RoleEmploye Role = "employe"
	RoleAgent   Role = "agent"
	RoleAdmin   Role = "admin"
)

func (r Role) valid() bool {
	return r == RoleEmploye || r == RoleAgent || r == RoleAdmin
}

// canProcess : l'utilisateur peut-il traiter des demandes ?
func (r Role) canProcess() bool {
	return r == RoleAgent || r == RoleAdmin
}

type User struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Request struct {
	ID           int       `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Requester    string    `json:"requester"`
	RequesterID  *int      `json:"requester_id"`
	AssigneeID   *int      `json:"assignee_id"`
	AssigneeName *string   `json:"assignee_name"`
	Status       Status    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	CommentCount int       `json:"comment_count"` // nombre de réponses
}

// Comment : une réponse dans le fil d'une demande
type Comment struct {
	ID         int       `json:"id"`
	RequestID  int       `json:"request_id"`
	AuthorID   *int      `json:"author_id"`
	AuthorName string    `json:"author_name"`
	AuthorRole Role      `json:"author_role"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}
