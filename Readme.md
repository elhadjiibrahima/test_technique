# Suivi des demandes internes

Petite application pour enregistrer des demandes internes et suivre leur avancement.

Avec la page web, on peut :
- ajouter une demande (titre, nom, description)
- voir la liste des demandes
- changer le statut d'une demande : ouverte, en cours ou terminée

## Comment lancer

```
go mod tidy
go run .
```

Ensuite, ouvrir http://localhost:8080 dans le navigateur.

Pour lancer les tests :

```
go test -v ./...
```

## Les fichiers

- `main.go` : lance le serveur et définit les routes
- `model.go` : la structure d'une demande et les statuts
- `database.go` : la connexion à la base SQLite
- `create.go` : ajouter une demande
- `list.go` : afficher la liste
- `update.go` : changer le statut
- `home.go` : afficher la page web
- `response.go` : envoyer une réponse en JSON
- `index.html` : la page web
- `main_test.go` : les tests

## Les routes

- `POST /requests` : ajouter une demande
- `GET /requests` : voir toutes les demandes
- `PATCH /requests/{id}` : changer le statut
- `GET /` : la page web

## Les tests

J'ai fait des tests automatiques pour vérifier que :
- les statuts valides sont acceptés et les autres refusés
- on peut créer une demande
- on a une erreur si le titre ou le nom manque
- la liste fonctionne (vide ou avec des demandes)
- on peut changer le statut
- on a une erreur si l'id ou le statut est faux

Pour les tests, j'utilise une base de données en mémoire. Comme ça, mes vraies données ne sont pas touchées.

## Pourquoi j'ai choisi Go

- c'est simple et rapide
- pas besoin de framework pour faire un serveur web
- on obtient un seul programme facile à lancer

## Pourquoi j'ai choisi SQLite

- pas de serveur à installer : la base est un simple fichier (`demandes.db`)
- les données restent quand on arrête le serveur
- c'est suffisant pour une petite application interne

J'ai pris le pilote `modernc.org/sqlite` parce qu'il marche sans installer de compilateur C, surtout sous Windows.

## Ce que je pourrais améliorer

- ajouter une connexion avec un mot de passe
- pouvoir supprimer ou modifier une demande
- afficher les demandes par pages si la liste devient longue