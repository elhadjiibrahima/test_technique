package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Test 1 : un statut valide est accepté, un faux statut est refusé
func TestStatusValid(t *testing.T) {
	if !StatusOpen.valid() {
		t.Error("open devrait être valide")
	}
	if Status("nimporte quoi").valid() {
		t.Error("un faux statut ne devrait pas être valide")
	}
}

// Test 2 : créer une demande sans titre doit donner une erreur 400
func TestCreateRequestSansTitre(t *testing.T) {
	body := strings.NewReader(`{"requester":"alice"}`)
	req := httptest.NewRequest("POST", "/requests", body)
	rec := httptest.NewRecorder()

	createRequest(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("code %d, attendu 400", rec.Code)
	}
}
