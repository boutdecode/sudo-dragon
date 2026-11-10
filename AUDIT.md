# Audit client / serveur et risques de triche

## Contexte
Cartographier chaque fonctionnalité de FEATURES.md (serveur, client, ou les deux) et évaluer le risque de triche. Analyse faite par lecture du code, sans exécution : « vérifié » = lu, « supposé » = déduit.

## 1. Répartition par fonctionnalité

| Fonctionnalité | Où | Autorité | Risque si client modifié |
|---|---|---|---|
| Monde, terrain, lieux, routes, climats, niveau de région, contrées | **Les deux** (`internal/world`, déterministe depuis la seed) | Serveur pour collisions | Faible : le client voit tout le monde (map hack), sans effet serveur |
| Donjons : entrées, plans, palier | **Les deux** (plan recalculé des deux côtés) | Serveur pour instance, monstres, coffres, boss | Moyen (spoiler) : toutes les entrées et tous les plans sont lisibles, le « secret » est cosmétique |
| Instance, état, reset des donjons, coffres, butin | **Serveur** | Serveur | Aucun levier client |
| Découverte des entrées (`discoveries`) | **Serveur** (mémorise) + client (affichage) | Serveur | Cosmétique : marcher sur la case suffit à entrer |
| Déplacement | **Serveur** (cooldown 2 ticks, collisions) ; client envoie seulement dx/dy ∈ {-1,0,1} | Serveur | Pas de speedhack ni téléportation (vérifié) |
| `/goto`, pathfinding A* | **Client** seulement | Serveur juge les pas | Aucun : produit des `move` normaux |
| Carte `M`, repères, brouillard, `map.json` | **Client** seulement | n/a | Local (fichier non borné en taille) |
| Rendu, FX, i18n affichage, illustrations | **Client** | n/a | Aucun |
| Monstres, IA, meutes, garnisons, mise à l'échelle | **Serveur** | Serveur | Aucun |
| Combat (dés, initiative, dégâts, fuite, potions) | **Serveur** ; client = affichage et choix d'action | Serveur | Aucun : tous les jets côté serveur (mais voir RNG) |
| Personnage, création, tirage 3d6, niveaux, mana | **Serveur** | Serveur | Reroll contournable par reconnexion |
| Inventaire, équipement, boutiques, prix, charisme, réputation | **Serveur** | Serveur | Pas de duplication ni de prix négatif trouvés |
| PNJ, services, quêtes (validation, récompenses) | **Serveur** | Serveur (le modèle ne fait que proposer) | Aucun levier ; PNJ placés de façon déterministe donc connus du client |
| Dialogues LLM, lore, rumeurs | **Serveur** | Serveur | Prompt injection possible mais sans pouvoir sur le jeu |
| Chat, commandes, MP | **Serveur** relaie ; client affiche | Serveur | Injection d'échappements (voir plus bas) |
| Comptes, quotas, TLS, persistance SQLite | **Serveur** | Serveur | Voir failles |

Bilan : l'architecture est saine, serveur autoritatif. Le client n'envoie aucune position, aucun or, aucun résultat de dé. Aucune triche d'économie ou de combat trouvée. Les risques sont surtout : fuite d'information (seed), abus réseau, texte non nettoyé.

## 2. Failles par sévérité

### Élevé
1. **Chat non nettoyé** (vérifié) : `server.go:621-627` fait seulement TrimSpace + troncature, et le client affiche tel quel (`model.go:649-652`, `936-953`). Un joueur peut envoyer ANSI/OSC/bidi/fausses lignes à tous (`/g`, local, `/w`). `textsafe.Clean` n'est appliqué qu'aux répliques PNJ.
2. **Quotas login/inscription contournables en parallèle** (vérifié) : `blocked(ip)` testé avant bcrypt, compteur incrémenté après (`server.go:268-304`, `292`). N connexions simultanées = N essais ou N comptes. Seul le sémaphore bcrypt freine.

### Moyen
3. **Pas de limite de débit** sur `move`, `combat_action`, `ping`, et `buy/sell/heal/talk/turnin` ; inbox de 1024 (`game.go:296`) : un flood fait attendre les `Join/Leave/Act` bloquants des autres. Pas de plafond de connexions. Impact exact supposé.
4. **Seed publique + RNG dérivé de la seed** (`game.go:197`, `Welcome.Seed`) : entrées/plans de donjon lisibles (accepté), mais jets de dés potentiellement prédictibles (supposé) car le flux est partagé et les jets s'affichent.
5. **Aucun niveau requis pour entrer dans un donjon** (`game.go:529`) : un niveau 1 atteint un palier 5 et ses coffres.
6. **Pas de `recover()`** dans game/server : une panique dans la boucle de jeu tue le serveur (aucun chemin concret trouvé).
7. **Double connexion** : chargement avant réservation du pseudo (`server.go:308` vs `335-346`), fenêtre étroite, risque de perte de progression (pas de duplication).
8. **Budget LLM global** (1 M jetons/jour) épuisable avec de nombreux comptes ; `Stock` sans limite par compte/IP.
9. **Mot de passe en clair sur `ws://`** ; le client n'avertit pas hors localhost (`net.go`, défaut `ws://localhost:8080/ws`).

### Faible
10. Déconnexion en combat = fuite gratuite à 100 % (`leave` → `abortFight`).
11. PV d'ouverture de combat non synchronisés (`fight.go:80`) ; potions de butin écrasées en combat de groupe (`fight.go:130,185`) : bugs d'intégrité.
12. Rerolls de stats illimités par reconnexion avant validation.
13. `enterDungeon` entraîne de force les joueurs libres à 5 cases (grief).
14. Énumération de comptes à l'inscription ; collisions Unicode `NOCASE` vs `EqualFold`.
15. `map.json` : `Explored` sans borne à la lecture (local).
16. `clientIP` = `RemoteAddr` seulement (proxy = quota partagé) ; `/stats` public ; pas de kick d'inactifs ; `allowSay` partagé gêne l'équipement rapide.
17. LLM : rumeurs d'un joueur injectées dans les prompts d'autres joueurs (garde-fous : nettoyage, rejet de liens, modération fermée) ; souvenir privé non modéré ; alias imitable.

### Vérifié sans défaut
Pas de speedhack/téléport, pas de prix ou quantité négatifs, or plafonné, indices contrôlés, pas d'IDOR, SQL paramétré, snapshots limités à la vue, boucle de jeu mono-goroutine, login sans énumération de comptes.

Non audité en détail : `store/{quests,gear,lore,discoveries}.go`, `world/*`, `gear/{loot,fallback}.go`, `llm/mistral.go`.

## 3. Correctifs proposés (ordre de priorité)
1. `textsafe.Clean(text, MaxChatLen)` dans `readLoop`/`handleChat` (`server.go`, `chat.go`) + nettoyage à la réception dans `client/model.go`.
2. Réserver le quota login/inscription avant bcrypt (`server.go`, `limiter.go`), limiter les connexions non authentifiées par IP.
3. Limiteur de débit par session pour `move`, `combat_action`, `ping`, `npc_action` ; plafond de connexions.
4. Initialiser le RNG du jeu avec `crypto/rand` (`game.go:197`).
5. Avertir ou refuser `ws://` hors localhost dans le client (`client/net.go`).
6. Réserver le pseudo avant `LoadCharacter` ; `recover()` autour des tâches de la boucle de jeu.
7. Corriger les bugs d'intégrité de combat (points 10-11), borner `Explored` à la lecture.
8. Décision de design : exiger le niveau du palier (ou la découverte) pour entrer dans un donjon.

## Vérification
Tests unitaires par correctif (`make test`, `make race`, `make check` avec Go 1.27 dans `~/sdk/go1.27.3/bin`), test de flood avec `make load`, et mise à jour de FEATURES.md dans le même commit (règle AGENTS.md).
