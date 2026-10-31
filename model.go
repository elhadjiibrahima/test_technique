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
	JobTitle  string    `json:"job_title"` // poste occupé, ex : « Comptable »
	Phone     string    `json:"phone"`
	Address   string    `json:"address"`
	Active    bool      `json:"active"`           // false = compte désactivé (connexion impossible)
	Online    bool      `json:"online,omitempty"` // une page ouverte en ce moment (liste admin)
}

// Colonnes lues pour un utilisateur (jamais password_hash)
const userColumns = `id, name, email, role, created_at, job_title, phone, address, active`

// scanUser lit une ligne produite avec userColumns
func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &u.JobTitle, &u.Phone, &u.Address, &u.Active)
	return u, err
}

type Request struct {
	ID           int        `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Requester    string     `json:"requester"`
	RequesterID  *int       `json:"requester_id"`
	RequesterJob *string    `json:"requester_job_title"` // poste du demandeur
	RequesterTel *string    `json:"requester_phone"`     // téléphone du demandeur
	AssigneeID   *int       `json:"assignee_id"`
	AssigneeName *string    `json:"assignee_name"`
	Status       Status     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Priority     Priority   `json:"priority"`
	Category     Category   `json:"category"`
	CommentCount int        `json:"comment_count"`    // nombre de réponses
	FileCount    int        `json:"attachment_count"` // nombre de pièces jointes
	ResolvedAt   *time.Time `json:"resolved_at"`      // date du passage à « terminée »
	Rating       *int       `json:"rating"`           // note du demandeur, de 1 à 5
	RatingNote   string     `json:"rating_comment"`   // commentaire avec la note
}

// Priority : l'urgence d'une demande
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityUrgent Priority = "urgent"
)

var priorityLabels = map[Priority]string{
	PriorityLow:    "basse",
	PriorityNormal: "normale",
	PriorityUrgent: "urgente",
}

func (p Priority) valid() bool { _, ok := priorityLabels[p]; return ok }

// Category : le type de demande
type Category string

var categoryLabels = map[Category]string{
	"informatique": "Informatique",
	"materiel":     "Matériel et fournitures",
	"rh":           "Ressources humaines",
	"logistique":   "Logistique et locaux",
	"autre":        "Autre",
}

func (c Category) valid() bool { _, ok := categoryLabels[c]; return ok }

// normalizeMeta met les valeurs par défaut et vérifie priorité et catégorie
func normalizeMeta(req *Request, errs map[string]string) {
	if req.Priority == "" {
		req.Priority = PriorityNormal
	}
	if req.Category == "" {
		req.Category = "autre"
	}
	if !req.Priority.valid() {
		errs["priority"] = "Priorité invalide : basse, normale ou urgente."
	}
	if !req.Category.valid() {
		errs["category"] = "Catégorie invalide."
	}
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
