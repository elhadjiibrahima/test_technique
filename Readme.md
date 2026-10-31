# Suivi des demandes internes

Petite application pour enregistrer des demandes internes et suivre leur avancement,
avec des comptes, des rôles et des notifications.

Avec la page web, on peut :
- créer un compte et se connecter
- ajouter une demande (titre, description, priorité, catégorie, pièces jointes)
- suivre où en est sa demande : ouverte, en cours ou terminée, et qui la traite
- recevoir une notification (🔔) quand sa demande est prise en charge, change de statut ou reçoit une réponse
- échanger avec l'équipe dans le fil de réponses (💬) de chaque demande
- joindre des photos ou des PDF (📎) et voir l'historique complet (🕓) de chaque demande
- en cas de mot de passe oublié, recevoir un lien de réinitialisation de l'admin
- pour les agents : prendre en charge une demande, changer son statut, la libérer
- pour les admins : assigner les demandes, gérer les comptes et les rôles
- filtrer la liste (statut, priorité, catégorie, « mes demandes à traiter », « non assignées »),
  trier les urgentes d'abord, et la parcourir page par page

## Les rôles

| Action | Employé | Agent | Admin |
| --- | :---: | :---: | :---: |
| Créer une demande | ✅ | ✅ | ✅ |
| Voir les demandes | les siennes | toutes | toutes |
| Modifier / supprimer une demande | la sienne, tant que personne ne l'a prise en charge | ❌ | ✅ |
| Prendre en charge une demande libre | ❌ | ✅ | ✅ |
| Changer le statut | ❌ | si elle est libre ou à lui | ✅ |
| Répondre sur une demande | la sienne | ✅ | ✅ |
| Ajouter une pièce jointe | à la sienne | ✅ | ✅ |
| Supprimer une pièce jointe | celles qu'il a ajoutées | celles qu'il a ajoutées | ✅ |
| Changer la priorité | à la création, puis tant que personne ne l'a prise | si elle est libre ou à lui | ✅ |
| Générer un lien de réinitialisation de mot de passe | ❌ | ❌ | ✅ |
| Assigner une demande à un agent | ❌ | ❌ | ✅ |
| Gérer les comptes et les rôles | ❌ | ❌ | ✅ |

Un compte créé depuis la page est toujours **employé**. Un admin peut ensuite le passer agent ou admin.

## Priorité, catégorie, historique et pièces jointes

- **Priorité** : basse, normale ou urgente. Une demande urgente est signalée « 🔴 URGENT » dans la notification des agents.
- **Catégorie** : informatique, matériel et fournitures, ressources humaines, logistique et locaux, autre.
- **Historique** : chaque action est notée avec son auteur et sa date (création, modification, prise en charge,
  assignation, changement de statut ou de priorité, ajout ou suppression de pièce jointe).
- **Pièces jointes** : images (PNG, JPEG, GIF, WebP) et PDF, 5 Mo maximum, 5 fichiers par demande.
  Le type est vérifié à partir du contenu du fichier, pas de son nom. Les images s'affichent, les PDF se téléchargent.
  Les fichiers sont stockés dans la base SQLite.

## Mot de passe oublié

Le site n'envoie pas encore d'e-mails, donc la réinitialisation passe par un administrateur :
1. sur la page de connexion, l'utilisateur clique « Mot de passe oublié ? » et donne son e-mail ;
2. les admins reçoivent une notification. Dans **Utilisateurs**, le bouton 🔑 génère un lien
   valable 24 heures et utilisable une seule fois, à transmettre à l'utilisateur (WhatsApp, SMS…) ;
3. l'utilisateur ouvre le lien et choisit un nouveau mot de passe. Ses anciennes sessions sont fermées.

La page répond la même chose que le compte existe ou non, pour ne pas révéler quels e-mails sont inscrits.

## Les notifications

- nouvelle demande → les agents et les admins sont prévenus
- demande assignée → l'agent et le demandeur sont prévenus
- changement de statut → le demandeur est prévenu
- réponse de l'admin ou d'un agent → le demandeur est prévenu
- réponse du demandeur → l'agent qui traite la demande est prévenu (ou tous les agents et admins si personne ne la traite)
- pièce jointe ajoutée par l'équipe → le demandeur est prévenu ; ajoutée par le demandeur → l'agent est prévenu
- mot de passe oublié → les admins sont prévenus
- demande modifiée ou supprimée par un admin → le demandeur est prévenu
- rôle changé → l'utilisateur est prévenu

La page vérifie les nouvelles notifications toutes les 15 secondes.

## Comment lancer

```
go mod tidy
ADMIN_EMAIL=admin@exemple.com ADMIN_PASSWORD=unMotDePasseSolide go run .
```

Ensuite, ouvrir http://localhost:8080 dans le navigateur.

Variables d'environnement :
- `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` : le compte administrateur, créé au démarrage s'il n'existe pas.
  S'il existe déjà, il garde son mot de passe actuel. Sans ces variables, il n'y a pas d'admin.
- `PORT` : le port du serveur (8080 par défaut). Render la remplit tout seul.
- `DB_PATH` : le chemin du fichier SQLite (`demandes.db` par défaut).

Pour lancer les tests :

```
go test -v ./...
```

## Les fichiers

- `main.go` : lance le serveur et définit les routes (avec les rôles autorisés)
- `auth.go` : comptes, mots de passe, sessions, connexion et contrôle d'accès
- `users.go` : gestion des utilisateurs par l'admin
- `notifications.go` : envoi et lecture des notifications
- `comments.go` : les réponses sur les demandes
- `events.go` : l'historique des demandes
- `attachments.go` : les pièces jointes
- `reset.go` : mot de passe oublié et liens de réinitialisation
- `model.go` : les structures (demande, utilisateur), les statuts et les rôles
- `database.go` : la connexion à la base SQLite et la création des tables
- `create.go` : ajouter une demande
- `list.go` : afficher la liste et les compteurs
- `update.go` : changer le statut, assigner une demande
- `edit.go` : modifier ou supprimer une demande, règles de visibilité
- `validation.go` : contrôle de saisie
- `home.go` : afficher la page web
- `reponse.go` : envoyer une réponse en JSON (et les messages d'erreur communs)
- `index.html` : la page web
- `main_test.go` : les tests

## Les routes

Comptes :
- `POST /auth/register` : créer un compte (employé)
- `POST /auth/login` : se connecter
- `POST /auth/logout` : se déconnecter
- `GET /auth/me` : l'utilisateur connecté
- `PUT /auth/password` : changer son mot de passe
- `POST /auth/forgot` : signaler un mot de passe oublié (prévient les admins)
- `POST /auth/reset` : choisir un nouveau mot de passe avec le lien reçu

Demandes (il faut être connecté) :
- `POST /requests` : ajouter une demande
- `GET /requests` : voir les demandes visibles, les plus récentes en premier
  - `?page=2&limit=10` : pagination (le total est dans l'en-tête `X-Total-Count`)
  - `?status=open` : seulement un statut (`open`, `in_progress` ou `done`)
  - `?assignee=me` ou `?assignee=none` : mes demandes à traiter, ou les non assignées
  - `?priority=urgent` et `?category=informatique` : filtres
  - `?sort=priority` : les urgentes d'abord
- `GET /requests/stats` : le nombre de demandes par statut
- `PUT /requests/{id}` : modifier le titre et la description
- `DELETE /requests/{id}` : supprimer une demande
- `PATCH /requests/{id}` : changer le statut et/ou la priorité (agent, admin)
- `PATCH /requests/{id}/assign` : assigner ou libérer une demande (agent, admin)
- `GET /requests/{id}/comments` : le fil des réponses d'une demande
- `POST /requests/{id}/comments` : répondre (admin, agent, ou le demandeur)
- `GET /requests/{id}/history` : l'historique de la demande
- `GET /requests/{id}/attachments` : la liste des pièces jointes
- `POST /requests/{id}/attachments` : ajouter un fichier (formulaire multipart, champ `file`)
- `GET /attachments/{id}` : afficher ou télécharger un fichier
- `DELETE /attachments/{id}` : supprimer un fichier

Notifications :
- `GET /notifications` : les 30 dernières et le nombre de non lues
- `POST /notifications/read` : tout marquer comme lu

Utilisateurs (admin) :
- `GET /users` : la liste (`?role=agent` pour filtrer)
- `POST /users` : créer un compte avec un rôle
- `PATCH /users/{id}` : changer le rôle
- `DELETE /users/{id}` : supprimer un compte (ses demandes sont gardées)
- `POST /users/{id}/reset-link` : générer un lien de réinitialisation du mot de passe

## Sécurité

- les mots de passe sont hachés avec PBKDF2-SHA512 (210 000 itérations, sel aléatoire) : ils ne sont jamais stockés
- la session est un cookie `HttpOnly` (inaccessible au JavaScript), `SameSite=Lax`, et `Secure` en HTTPS ; en base on ne garde que l'empreinte du jeton
- les requêtes qui modifient des données doivent avoir l'en-tête `X-Requested-With: fetch`, ce qu'un autre site ne peut pas ajouter : ça protège contre le CSRF
- après 5 mots de passe faux pour un même e-mail, la connexion est bloquée 15 minutes
- un employé qui demande la demande d'un autre reçoit une erreur 404 : on ne révèle même pas qu'elle existe
- un admin ne peut pas se retirer ses propres droits ni supprimer son propre compte
- les pièces jointes sont servies avec une politique de sécurité (`Content-Security-Policy: sandbox`) qui empêche tout script de s'exécuter
- les liens de réinitialisation sont aléatoires (256 bits), valables 24 h, à usage unique, et seule leur empreinte est stockée

## Les tests

J'ai fait des tests automatiques pour vérifier, entre autres, que :
- le hachage des mots de passe marche (et ne contient pas le mot de passe)
- on peut s'inscrire, se connecter, se déconnecter et changer de mot de passe
- on ne peut pas s'inscrire en admin, ni deux fois avec le même e-mail
- la connexion est bloquée après trop d'échecs, et les requêtes sans l'en-tête CSRF sont refusées
- un employé ne voit et ne modifie que ses demandes, et ne peut pas les traiter
- un agent prend en charge une demande, et un autre agent ne peut pas la lui prendre
- l'admin assigne les demandes, gère les rôles, et ne peut pas se bloquer lui-même
- les bonnes personnes reçoivent les notifications
- l'admin peut répondre sur une demande, le demandeur est prévenu et peut répondre à son tour, et les autres employés ne voient pas ces réponses
- la priorité et la catégorie ont des valeurs par défaut, se filtrent, et le tri met les urgentes d'abord
- l'historique note chaque action, dans l'ordre, sans doublon
- les pièces jointes refusent les faux types, les fichiers vides ou trop gros, et sont protégées comme les demandes
- le lien de réinitialisation marche une seule fois, expire, et ferme les anciennes sessions
- la pagination et les filtres marchent, et refusent les mauvais paramètres

Pour les tests, j'utilise une base de données en mémoire. Comme ça, mes vraies données ne sont pas touchées.

Les tests se lancent aussi automatiquement sur GitHub à chaque push (GitHub Actions).

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

- garder les données sur Render : sur le plan gratuit, le fichier SQLite est effacé à chaque redéploiement
  (comptes et demandes compris, sauf le compte admin recréé au démarrage). Il faudrait un disque persistant avec `DB_PATH`, ou passer à PostgreSQL.
- envoyer aussi les notifications par e-mail, et le lien de réinitialisation automatiquement
- stocker les pièces jointes hors de la base (par exemple dans un service de stockage de fichiers) si elles deviennent nombreuses
- une recherche par mot-clé et un tableau de bord de statistiques pour l'admin
