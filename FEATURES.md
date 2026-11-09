# Fonctionnalités de sudo Dragon

Inventaire des fonctionnalités du jeu (anciennement « ASCII Adventure »), établi d'après le code. Pour l'installation, les options du serveur et l'exploitation, voir le [README](README.md).

## Monde et génération (`internal/world/`)

- **Monde infini** généré par bruit simplex depuis une seed, par chunks de 32×32.
- **Terrains** : eau profonde, eau, sable, plaine, forêt, collines, montagne, neige. Tuiles de structures : chemin, sol intérieur, mur, porte.
- **Lieux** : au plus un par région de 256×256.
  - **Village** (~32×26), **ville** (~58×46, avec muraille), **forteresse** (48×40).
  - Les plans sont recalculés à l'identique côté client et serveur : rien n'est envoyé ni stocké.
- **Contrées** : pays nommés aux frontières irrégulières (Voronoi), noms selon le terrain dominant, en français et en anglais.
- **Zones sûres** : village ou ville plus 3 cases de marge. Pas de monstres, pas de poursuite, régénération de 1 PV par seconde.

## Déplacement et exploration

- **Touches** : flèches, `Z Q S D`, `W A S D`, `h j k l`, diagonales `y u b n`.
- **Cadence** : 5 cases par seconde ; les monstres sont plus lents.
- **Vision** : ±40×±20 cases ; le serveur n'envoie que les entités qui ont changé.
- **Marche automatique `/moveto`** (`client/moveto.go`, `client/pathfind.go`) :
  - `/moveto x y`, ou par nom de repère ou de lieu découvert (casse ignorée, ambiguïtés signalées).
  - Recalcul du chemin après 4 ticks sans progrès, abandon après 8.
  - Interrompue par une direction, Échap, `q`, `i`, `c` ou `m`. Le serveur reste juge des collisions.
- **Carte du monde (`M`)** (`client/worldmap.go`) :
  - Zoom 1, 2, 4, 8 ou 16 tuiles par cellule (`+` / `-`), curseur clignotant aux flèches, `0` ou espace pour recentrer.
  - Seul le terrain exploré est affiché ; le reste est « inexploré » (`░`).
  - Légende (écran ≥ 90 colonnes) : vous, joueurs, village, forteresse, forêt, montagne, eau, route, repère, inexploré ; liste des repères et lieux proches avec direction et distance.
- **Repères** : `W` pose ou renomme (24 caractères, noms uniques, 9 au plus), `X` supprime.
- **Localisation** : l'en-tête affiche `contrée · lieu · terrain (x, y)` ; le journal annonce chaque nouvelle contrée ou nouveau lieu.

## Monstres (`internal/game/monsters.go`, `garrison.go`)

| Origine | Monstres |
|---|---|
| Biomes | rat (plaine), crabe (plage), loup (forêt), gobelin (collines), yéti (neige) |
| Forteresses | squelette, chevalier, seigneur de guerre (boss) |

- Errance et poursuite à portée de détection ; 500 monstres au plus, avec un quota par joueur.
- **Meutes** : un compagnon de plus tous les 3 niveaux du joueur ; des espèces compagnes apparaissent rarement.
- **Garnisons** : apparaissent à moins de 40 cases d'un joueur et restent dans la forteresse. Soldats : réapparition en 5 min ; seigneur : 15 min. Le seigneur laisse un trésor garanti (100 or, 3 potions).

## Combat (`internal/combat/`, `game/fight.go`, `client/combatview.go`)

- **Règles** : d20 + modificateur doit dépasser strictement la DEF. 20 naturel : touche et double les dés de dégâts ; 1 naturel : rate. La réduction d'armure est retirée des dégâts (minimum 1 si touché).
- **Groupes** : jusqu'à 3 monstres (2 pour les garnisons) ; les monstres libres compatibles à **5 cases** se joignent. Doublons numérotés (« Loup 1 », « Loup 2 »).
- **Initiative** : d20 + AGI. Les monstres strictement plus rapides frappent avant le premier tour ; à égalité, le joueur commence.
- **Actions** : Attaquer (`A`/`1`), Objet (`I`/`2`, potion +15 PV), Fuir (`F`/`3`, une chance sur deux). Flèches haut/bas + Entrée, gauche/droite pour changer de cible.
- **Interface** : deux colonnes (joueur / ennemis), détail de l'action, journal.
- **Illustrations** : rat, loup et crabe, en demi-blocs `▀` 24 bits (`render/image.go`). Les autres espèces n'en ont pas.
- **Secousse d'écran** (`client/fx.go`) : zone entière quand on est touché, colonne ennemie quand on touche ; 6 images de 60 ms (9 sur critique) ; PV qui clignotent en rouge. Désactivable avec `SUDO_DRAGON_NO_FX`.
- **Issue** :
  - Victoire : XP, or (moitié à totalité de l'XP du monstre), 30 % de chances d'une potion.
  - Défaite : retour au départ avec tous les PV, perte de 25 % de l'XP du niveau.
  - Inactivité de 60 s : fuite automatique.
- Les combattants sont immobiles et affichés en rouge pour les autres joueurs. Impossible d'équiper pendant un combat.

## Personnage (`internal/hero/`, `client/create.go`, `client/sheet.go`)

- **Caractéristiques** : FOR, AGI, INT, CON, SAG, CHA, tirées en 3d6 par le serveur.
- **Création** : 3 relances au total (`R`), puis choix de la classe.

| Classe | Dé de vie | Mana de départ |
|---|---|---|
| Guerrier | d12 | 2 |
| Gredin | d8 | 4 |
| Magicien | d6 | 8 |

- **Niveaux** : `10 × niveau` XP. À chaque niveau : dé de vie + modificateur de CON, PV et mana restaurés.
- **Mana** : base de classe + 2 par niveau + niveau × modificateur de SAG ; un sort coûte 2 mana.
- **Style d'attaque** déduit de l'arme : mêlée (FOR), distance (AGI), magie (INT). Sans mana, une arme magique sert de gourdin.
- **Charisme** : ±2 % par point de modificateur sur les prix, borné à ±10 %.
- **Fiche (`C`)** : classe, niveau, PV, mana, attaque, défense, or, position, palmarès (victoires, boss, morts), caractéristiques, équipement, réputation.
- Les anciens personnages repassent par la création à leur prochaine connexion.

## Inventaire, équipement et commerce (`internal/gear/`, `client/bag.go`, `game/shop.go`)

- **Emplacements** : arme, armure, bouclier, casque. **Rangs** 1 à 5 (niveau requis 1, 5, 10, 15, 20).
- **Armes** : dégâts `rang d famille + qualité` (coupant d4, tranchant d6, contondant d8, deux mains d12). Arc court, arbalète et bâton se portent à deux mains.
- **Armures** : bonus de DEF et dés de réduction (d4 légère, d6 intermédiaire, d8 lourde). Bouclier et casque : DEF seulement.
- Une arme à deux mains interdit le bouclier, et inversement.
- **Sac (`I`)** : 24 objets, `E` ou Entrée pour équiper/retirer. Revente à 40 % du prix ; un objet porté ne se revend pas.
- **Potions** : 2 au départ. **Or** plafonné à 999 999.
- **Forgeron (`F`, armes) et armurier (`R`)** : chaque ville a les deux, chaque village un seul, les forteresses aucun. Achat `1`–`9`, revente `V`. Prix modulés par la réputation.
- **Étalages générés par IA** : à la première visite, le modèle invente une spécialité et 6 à 8 objets ; le serveur valide tout et jette l'étalage s'il reste moins de 4 objets valides. L'étalage est le même pour tous. Sans clé Mistral, un assortiment de repli déterministe est proposé (`gear/fallback.go`).

## PNJ, dialogues et quêtes

- **PNJ** (`game/npc.go`) : marchand, soigneur, forgeron, armurier, chef de village / maire, villageois, villageois à quêtes, soldats. On leur parle en marchant dessus (abandon après 60 s).
- **Services** : potion à 20 or (revente 8 or) ; soins complets gratuits au niveau 1, ensuite 4 or × niveau. Le chef et le maire renseignent sur les lieux dans un rayon de 3 régions, même sans clé Mistral.
- **Dialogues libres** (Mistral, touche `D` / `T` en anglais) :
  - Le modèle ne produit que du texte, sans effet sur l'or, les objets ou les stats.
  - Modération des entrées, budget de jetons par jour, avis de confidentialité avec accord par compte.
  - Sans clé : fonction absente, phrase d'ambiance des villageois.
- **Mémoire et lore** (`lore/`) : fiche de présentation du joueur (8 lieux au plus), rumeurs (portée 3 régions, délai 5 min, fidélité décroissante, oubli après 14 jours), résumé privé par PNJ, faits vérifiés (niveau, boss, morts, quêtes rendues).
- **Quêtes** (`internal/quest/`, `client/quests.go`) :
  - Le modèle propose, le serveur décide : objectifs vérifiables, récompenses plafonnées, textes nettoyés. Accepter/Refuser avec `A`/`R`.
  - Objectifs : tuer 3 à 12 monstres d'une espèce, entrer dans un lieu voisin, vaincre le seigneur de guerre (niveau 10 minimum).
  - Limites : 5 quêtes ouvertes, une par donneur, 30 min entre deux quêtes d'un même donneur, expiration à 7 jours.
  - Livre de quêtes (`Q`), onglets avec `Tab`, abandon avec `X` deux fois ; on rend la quête au donneur.
- **Réputation** par région : Inconnu, Connu (3), Estimé (8), Honoré (15). Réduction de 20 % puis 40 % sur soins, potions et équipement.

## Chat et commandes

| Commande | Effet |
|---|---|
| texte libre | chat local (joueurs proches) |
| `/g`, `/global` | chat global |
| `/me` | émote locale |
| `/w`, `/msg`, `/whisper` pseudo message | message privé |
| `/moveto x y` ou `/moveto nom` | marche automatique (côté client) |
| `/help`, `/?` | aide |

Rafale de 5 messages puis 1 par seconde. `PgHaut` / `PgBas` font défiler l'historique, `Ctrl+↑` / `Ctrl+↓` redimensionnent la discussion.

## Touches clavier

| Contexte | Touches |
|---|---|
| Jeu | déplacement (voir plus haut), `Entrée` chat, `I` sac, `C` fiche, `M` carte, `Q` quêtes |
| Quitter | `Ctrl+C` |
| Connexion | `Tab` / `↑↓` changent de champ, `Ctrl+R` bascule connexion/création |
| Création | `R` relance, `↑↓←→` choix |
| Sac | `↑↓` / `jk`, `E` / Entrée, Échap ou `I` |
| Carte | flèches, `+` `-`, `0`, `W`, `X`, Échap ou `M` |
| Quêtes | `Tab`, `↑↓`, `X`, Échap ou `Q` |
| Combat | `A`/`1`, `I`/`2`, `F`/`3`, flèches, Entrée |
| PNJ | lettres traduites selon la langue |

## Interface et affichage (`internal/ui/`, `internal/render/`)

- Plein écran, fond sombre peint (option `--transparent` pour garder le fond du terminal), logo.
- Panneau latéral (jauges XP/PV/PM, caractéristiques) et zone de discussion redimensionnable.
- Terrain avec variantes et couleurs ; joueurs en combat affichés en rouge.
- Latence mesurée par ping/pong et affichée.
- Options du client : `-addr`, `-name`, `-transparent`, `-lang`.

## Sauvegarde et persistance (`internal/store/`)

- **SQLite** en mode WAL : comptes, personnages, sac, quêtes, réputation, lore, étalages (`npc_wares`).
- Écriture asynchrone ; sauvegarde à la déconnexion, après chaque combat, toutes les 30 s, à l'arrêt propre.
- La seed est fixée à la création de la base ; la changer sur une base existante est refusé.
- **Côté client** : carte, repères et exploration dans `<UserConfigDir>/sudo-dragon/map.json` (une entrée par seed, écriture différée de 15 s). Le serveur n'en sait rien.
- Non persistés : le monde (recalculé de la seed) et les garnisons (réinitialisées au redémarrage).

## Réseau, multijoueur et exploitation (`internal/server/`, `internal/protocol/`)

- **WebSocket**, enveloppes JSON ; serveur autoritatif à 10 Hz, un seul goroutine pour l'état du jeu.
- Monde partagé : joueurs visibles en temps réel, chat, messages privés.
- Instantanés fusionnés : un client lent saute des images au lieu d'être déconnecté.
- **Comptes** : pseudo 3–16 caractères, mot de passe 8–72 octets (bcrypt), message d'erreur identique pour pseudo inconnu et mauvais mot de passe.
- **Quotas par IP** : 8 échecs de connexion par minute, 10 créations de compte par heure ; limites du chat et des répliques aux PNJ.
- **TLS** (`wss://`) via `-tls-cert` / `-tls-key` ; sur `ws://`, le mot de passe voyage en clair.
- **Supervision** : `/healthz`, `/stats` (JSON), `-pprof`. Administration : `-lore-list`, `-lore-purge`, `-wares-list`, `-wares-reset`.
- **Test de charge** : `make load N=…` et `cmd/bot`.

## Internationalisation (`internal/i18n/`)

- Français et anglais ; langue déduite de `LC_ALL`, `LC_MESSAGES`, `LANGUAGE`, `LANG`, ou imposée par `-lang`. Toute autre langue donne l'anglais.
- Le serveur sert chaque joueur dans sa langue (erreurs, aide, combat, PNJ, consigne au modèle). Logs et options du serveur restent en français.
- Catalogues `fr.go` / `en.go` avec test de parité des clés.

## Outils

| Binaire | Rôle |
|---|---|
| `cmd/server` | serveur de jeu |
| `cmd/client` | interface TUI (Bubble Tea v2, Lip Gloss) |
| `cmd/bot` | joueurs automatiques pour les tests de charge |
| `cmd/worldview` | dessine une zone du monde en ASCII (`-sites`, `-territories`) |

`Makefile` : build, test, race, check, load, llm-smoke. Go 1.27 requis.

## En cours et limites

- **Routes entre lieux** (`internal/world/roads.go`) : en cours, non commité (A* entre régions voisines, ponts sur eau peu profonde, jamais sur eau profonde ni montagne).
- Pas de compétences ni de sorts à apprendre : la magie est un style d'attaque lié à l'arme.
- Pas de reconnexion automatique après une coupure.
- Un seul processus serveur, sans montée en charge horizontale.
- Le README est en partie périmé (Défendre, limites connues, carte, i18n).
