package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxFileSize        = 5 << 20 // 5 Mo par fichier
	maxFilesPerRequest = 5
)

// Types de fichiers acceptés. Le type est détecté à partir du contenu du
// fichier, pas de son nom : renommer un .exe en .png ne passe pas.
var allowedTypes = map[string]bool{
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"application/pdf": true,
}

type Attachment struct {
	ID          int       `json:"id"`
	RequestID   int       `json:"request_id"`
	UploaderID  *int      `json:"uploader_id"`
	Uploader    string    `json:"uploader"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int       `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
}

// GET /requests/{id}/attachments : la liste des pièces jointes (sans leur contenu)
func listAttachments(w http.ResponseWriter, r *http.Request, u User) {
	req, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}
	rows, err := db.Query(
		`SELECT id, request_id, uploader_id, uploader, filename, content_type, size, created_at
		 FROM attachments WHERE request_id = ? ORDER BY id`, req.ID)
	if err != nil {
		serverError(w)
		return
	}
	defer rows.Close()

	list := []Attachment{}
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.RequestID, &a.UploaderID, &a.Uploader, &a.Filename,
			&a.ContentType, &a.Size, &a.CreatedAt); err != nil {
			serverError(w)
			return
		}
		list = append(list, a)
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /requests/{id}/attachments : ajouter un fichier (champ "file" d'un formulaire multipart).
// Images (PNG, JPEG, GIF, WebP) et PDF, 5 Mo maximum, 5 fichiers maximum par demande.
func uploadAttachment(w http.ResponseWriter, r *http.Request, u User) {
	req, ok := loadVisibleRequest(w, r, u)
	if !ok {
		return
	}
	badRequest := func(msg string) {
		writeErrors(w, http.StatusBadRequest, map[string]string{"file": msg})
	}

	if req.FileCount >= maxFilesPerRequest {
		badRequest(fmt.Sprintf("Cette demande a déjà %d pièces jointes (maximum).", maxFilesPerRequest))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+(512<<10))
	file, header, err := r.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			badRequest("Le fichier est trop gros (5 Mo maximum).")
		} else {
			badRequest("Aucun fichier reçu.")
		}
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		badRequest("Le fichier n'a pas pu être lu.")
		return
	}
	switch {
	case len(data) == 0:
		badRequest("Le fichier est vide.")
		return
	case len(data) > maxFileSize:
		badRequest("Le fichier est trop gros (5 Mo maximum).")
		return
	}

	contentType := http.DetectContentType(data)
	if !allowedTypes[contentType] {
		badRequest("Type de fichier non accepté : seulement des images (PNG, JPEG, GIF, WebP) ou des PDF.")
		return
	}

	a := Attachment{
		RequestID:   req.ID,
		UploaderID:  &u.ID,
		Uploader:    u.Name,
		Filename:    cleanFilename(header.Filename, contentType),
		ContentType: contentType,
		Size:        len(data),
		CreatedAt:   time.Now(),
	}
	res, err := db.Exec(
		`INSERT INTO attachments (request_id, uploader_id, uploader, filename, content_type, size, data, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.RequestID, u.ID, a.Uploader, a.Filename, a.ContentType, a.Size, data, a.CreatedAt)
	if err != nil {
		serverError(w)
		return
	}
	id, _ := res.LastInsertId()
	a.ID = int(id)

	logEvent(req.ID, u, "a ajouté la pièce jointe « "+a.Filename+" »")
	if isOwner(u, req) {
		notifyUnlessActor(req.AssigneeID, u, &req.ID,
			fmt.Sprintf("%s a ajouté une pièce jointe à la demande #%d « %s ».", u.Name, req.ID, req.Title))
	} else {
		notifyUnlessActor(req.RequesterID, u, &req.ID,
			fmt.Sprintf("%s a ajouté une pièce jointe à votre demande #%d « %s ».", u.Name, req.ID, req.Title))
	}

	writeJSON(w, http.StatusCreated, a)
}

// loadAttachment lit la pièce jointe de l'URL et vérifie que l'utilisateur voit sa demande
func loadAttachment(w http.ResponseWriter, r *http.Request, u User, withData bool) (Attachment, Request, []byte, bool) {
	id, ok := parseID(w, r)
	if !ok {
		return Attachment{}, Request{}, nil, false
	}
	var a Attachment
	var data []byte
	cols := `id, request_id, uploader_id, uploader, filename, content_type, size, created_at`
	dest := []any{&a.ID, &a.RequestID, &a.UploaderID, &a.Uploader, &a.Filename, &a.ContentType, &a.Size, &a.CreatedAt}
	if withData {
		cols += `, data`
		dest = append(dest, &data)
	}
	err := db.QueryRow(`SELECT `+cols+` FROM attachments WHERE id = ?`, id).Scan(dest...)

	var req Request
	if err == nil {
		req, err = getRequest(a.RequestID)
	}
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !canSee(u, req)) {
		writeErrors(w, http.StatusNotFound, map[string]string{"general": "Cette pièce jointe n'existe pas."})
		return Attachment{}, Request{}, nil, false
	}
	if err != nil {
		serverError(w)
		return Attachment{}, Request{}, nil, false
	}
	return a, req, data, true
}

// GET /attachments/{id} : afficher (images) ou télécharger (PDF) une pièce jointe
func downloadAttachment(w http.ResponseWriter, r *http.Request, u User) {
	a, _, data, ok := loadAttachment(w, r, u, true)
	if !ok {
		return
	}
	disposition := "inline"
	if !strings.HasPrefix(a.ContentType, "image/") || r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	h := w.Header()
	h.Set("Content-Type", a.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.Filename}))
	// Le fichier est affiché sans pouvoir exécuter de script
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, max-age=3600")
	w.Write(data)
}

// DELETE /attachments/{id} : supprimer une pièce jointe (celui qui l'a ajoutée, ou un admin)
func deleteAttachment(w http.ResponseWriter, r *http.Request, u User) {
	a, req, _, ok := loadAttachment(w, r, u, false)
	if !ok {
		return
	}
	if u.Role != RoleAdmin && (a.UploaderID == nil || *a.UploaderID != u.ID) {
		forbidden(w)
		return
	}
	if _, err := db.Exec(`DELETE FROM attachments WHERE id = ?`, a.ID); err != nil {
		serverError(w)
		return
	}
	logEvent(req.ID, u, "a supprimé la pièce jointe « "+a.Filename+" »")
	w.WriteHeader(http.StatusNoContent)
}

// cleanFilename garde un nom de fichier lisible et sans danger :
// pas de chemin, pas de caractères spéciaux invisibles, 100 caractères maximum,
// et une extension qui correspond au vrai type du fichier.
func cleanFilename(name, contentType string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' || r == '/' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)

	ext := map[string]string{
		"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif",
		"image/webp": ".webp", "application/pdf": ".pdf",
	}[contentType]
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base == "" || base == "." {
		base = "fichier"
	}
	if utf8.RuneCountInString(base) > 100 {
		base = string([]rune(base)[:100])
	}
	return base + ext
}
