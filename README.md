# sudo Dragon

Un RPG multijoueur en monde ouvert, à l'ancienne, entièrement en ASCII dans le terminal.
Interface avec [Bubble Tea](https://charm.land/) et Lip Gloss ; serveur autoritatif en Go.

```
╭───────────────────────────────────────────────╮╭────────────────────────╮
│"'"""""""""'"""""""'".,:~~~~~~~--~~~~~~~~~~~~~~~││hero                    │
│""""'""""""""'""".'"".""..:.~~~~~-~~~~~~~~~~~~~~~││Pos  4, 2               │
│"""""""."""""".""."."'""""""..~~~~~~~~~~~~~~~~~~~││Sol  Plaine             │
│""".""""""""""r"""""""""".'""",.~~-~~~~~~~~~~~~~~││Niv 1   XP 4/10         │
│""""""""'"'""."".""""'"'""'""".,.~~~~~~~~~~~~~~~~││PV ########## 30/30     │
│"""""""""""""""@""""""""""""""..~~--~~~~~~~~~~~~~││Atq 6  Déf 2  Pot 2     │
╰───────────────────────────────────────────────╯╰────────────────────────╯
* hero a rejoint le monde
Entrée: chat (/help) · flèches/ZQSD/hjkl: bouger · Ctrl+C: quitter
```

*(illustration schématique, les couleurs ne sont pas rendues ici)*

- **Monde infini** généré par bruit simplex à partir d'une seed : océans, plages, plaines, forêts, collines, montagnes, neige.
- **Villages, villes et forteresses** générés eux aussi depuis la seed : zones sûres, marchands et soigneurs dans les premiers, garnisons et seigneur de guerre dans les secondes.
- **Multijoueur** : tout le monde partage le même monde, se voit, discute et se déplace en temps réel.
- **Monstres** par biome qui errent et vous poursuivent ; **combat au tour par tour** avec potions, fuite, XP et niveaux.
- **Or et boutiques** : les monstres laissent de l'or, qui s'échange contre des potions ou des soins.
- **Comptes et sauvegarde** : pseudo et mot de passe (bcrypt), personnage persistant (SQLite).

## Démarrage rapide

Prérequis : **Go 1.27 ou plus** (`go version`). Sur une machine où `go` est plus ancien,
indiquez la bonne version : `make GO=/chemin/vers/go1.27/bin/go build`.

```sh
make build                 # compile serveur, client, bot et outil de carte dans ./bin

./bin/sudo-dragon-server               # écoute sur :8080, base ./sudo-dragon.db
./bin/sudo-dragon               # dans un autre terminal (ou plusieurs, pour jouer à plusieurs)
./bin/sudo-dragon --transparent   # garde le fond de votre terminal (sinon le fond sombre du thème est peint)
```

> Le jeu s'appelait « ASCII Adventure » : la base par défaut est désormais `sudo-dragon.db`. Pour garder une ancienne
> sauvegarde, renommez `ascii-adventure.db` ou lancez le serveur avec `-db ascii-adventure.db`.

Au premier lancement, appuyez sur **Ctrl+R** pour passer en « Création de compte », choisissez un
pseudo et un mot de passe (8 à 72 caractères), puis Entrée. Les fois suivantes, connectez-vous
simplement avec les mêmes identifiants : vous reprenez là où vous étiez.

Pour jouer sur un autre serveur : `./bin/sudo-dragon -addr wss://jeu.exemple.org/ws -name monpseudo`.

## Jouer

**Se déplacer** : flèches, `Z Q S D` (AZERTY), `W A S D`, ou `h j k l`. Diagonales : `y u b n`.
Un joueur avance de 5 cases par seconde ; tous les monstres sont plus lents, on peut donc toujours leur échapper.

**Chat** : `Entrée` ouvre la saisie, `Entrée` envoie, `Échap` annule. `PgHaut`/`PgBas` font défiler l'historique.

| Saisie | Effet |
|---|---|
| `message` | chat **local** : seuls les joueurs proches (dans votre zone de vision) le voient |
| `/g message` | chat **global**, tout le monde |
| `/me action` | émote locale |
| `/w pseudo message` | message **privé** |
| `/help` | rappel des commandes |

**Combattre** : marchez sur un monstre pour l'attaquer, ou laissez-le vous toucher. L'écran de combat propose :
`A` attaquer · `D` défendre (dégâts réduits de moitié) · `I` boire une potion (+15 PV) · `F` fuir (une chance sur deux).
Les touches `1` à `4` font la même chose. Après la victoire, `Entrée` ferme l'écran.

- Vainqueur : de l'XP, et 30 % de chances de trouver une potion. Chaque niveau donne +6 PV, +2 attaque, +1 défense, et restaure tous les PV.
- Vaincu : vous réapparaissez au point de départ avec tous vos PV, en perdant 25 % de l'XP du niveau (jamais de niveau).
- Les PV remontent d'un point toutes les 3 secondes hors combat.
- Pendant un combat, vous et votre adversaire restez sur place, hors d'atteinte des autres, et apparaissez en rouge pour les autres joueurs.
  Un joueur inactif depuis 60 secondes est considéré comme ayant fui.

| Monstre | Glyphe | Habitat | PV | Atq | Déf | XP | Détection |
|---|---|---|---|---|---|---|---|
| Rat | `r` | plaine | 8 | 3 | 0 | 4 | 5 cases |
| Crabe | `c` | plage | 12 | 4 | 3 | 6 | 3 cases |
| Loup | `w` | forêt | 16 | 6 | 1 | 10 | 9 cases |
| Gobelin | `g` | collines | 22 | 7 | 2 | 15 | 8 cases |
| Yéti | `Y` | neige | 34 | 10 | 4 | 30 | 7 cases |
| Squelette | `S` | forteresse | 36 | 10 | 4 | 25 | 7 cases |
| Chevalier | `K` | forteresse | 60 | 14 | 8 | 50 | 7 cases |
| Seigneur de guerre | `B` | forteresse | 180 | 22 | 12 | 250 | 6 cases |

Un monstre vaincu laisse de l'or : entre la moitié et la totalité de son XP.

Un personnage débute niveau 1 avec 30 PV, 6 d'attaque, 2 de défense, 2 potions et aucun or ; il faut `10 × niveau` XP pour passer au niveau suivant.
Un débutant bat un rat ou un loup, mais pas un yéti : il faut progresser (c'est vérifié par un test d'équilibrage).

## Villages, villes et forteresses

Le monde est découpé en régions de 256 × 256 cases ; chacune accueille **au plus un lieu** (un peu plus d'une région sur deux en a un),
choisi selon le terrain : les villages en plaine ou en forêt, les villes en plaine et de préférence près de l'eau, les forteresses sur les collines.
Un village se trouve toujours à quelques dizaines de cases du point de départ. Les lieux sont **calculés à l'identique par le serveur et par le client**
à partir de la seed : rien n'est envoyé ni sauvegardé.

| Lieu | Taille | Contenu |
|---|---|---|
| Village | ~32 × 26 | 4 à 8 maisons reliées à une place par des chemins, un **soigneur** et 3 **villageois** |
| Ville | ~58 × 46 | muraille à quatre portes, deux avenues, une place, une vingtaine de bâtiments, un **marchand**, un **soigneur** et 5 **villageois** |
| Forteresse | 48 × 40 | enceinte épaisse à tours d'angle et un portail, cour, deux casernes, donjon cloisonné ; **8 soldats et le seigneur de guerre** |

Sur la carte : `#` mur, `+` porte, `:` chemin, `_` sol intérieur, `M` marchand, `H` soigneur, `V` villageois (sans métier : on ne fait que discuter avec eux ; sans clé Mistral ils disent une phrase d'ambiance).

**Zones sûres.** Un village ou une ville, avec trois cases de marge, est une zone sûre : aucun monstre n'y apparaît ni n'y entre,
un joueur qui s'y trouve n'est ni poursuivi ni attaqué, et il y regagne 1 PV par seconde (au lieu d'un toutes les 3 secondes).

**PNJ.** Marchez sur un PNJ pour lui parler (vous restez immobile pendant la conversation ; `Échap` pour la quitter, abandon automatique après 60 secondes).

| PNJ | Action | Prix |
|---|---|---|
| Marchand | `A` acheter une potion | 20 or |
| Marchand | `V` vendre une potion | +8 or |
| Soigneur | `S` soins complets | gratuit au niveau 1, ensuite 4 or × niveau |

Le serveur décide des prix et des refus (or insuffisant, sac plein, déjà en pleine forme : rien n'est alors débité). Chaque transaction est sauvegardée aussitôt.

**Forteresses.** La garnison apparaît quand un joueur s'approche (à moins de 40 cases) et ne quitte jamais la forteresse : fuir suffit à lui échapper.
Chaque soldat occupe un poste ; vaincu, il n'est remplacé qu'au bout de **5 minutes** (15 pour le seigneur de guerre). S'éloigner ne déclenche aucun délai.
Le **seigneur de guerre**, au centre du donjon, laisse un trésor garanti : **100 or et 3 potions**. Ordre de grandeur, un joueur à l'aise contre
un squelette vers le niveau 4, un chevalier vers le 8, et le seigneur de guerre vers le 16 (ces seuils sont verrouillés par des tests d'équilibrage).

`go run ./cmd/worldview -seed 1 -sites -x 0 -y 0 -w 400 -h 0` liste les lieux d'une zone ; avec une hauteur non nulle, il les dessine
(`S` soldat, `K` élite, `B` seigneur) pour préparer une expédition.

## PNJ conversationnels (Mistral)

Le choix **Discuter** (touche `D`) ouvre une conversation libre avec un PNJ. Le LLM ne produit que du texte : il n'agit jamais sur
l'or, les objets ou les stats ; le commerce et les soins restent dans les menus fixes. Sans clé, la fonction est absente et le jeu
fonctionne normalement.

**Activation** : `export MISTRAL_API_KEY=...` puis lancer le serveur (`-llm auto` par défaut ; `-llm on` exige la clé ; `-llm off` désactive).
La clé ne se donne jamais en option. Au démarrage, une sonde teste le chat et la modération ; si l'API est en panne, le service reste
désactivé et se re-teste toutes les 5 minutes. Options : `-llm-model`, `-llm-moderation-model` (les noms de modèles changent : voir la
documentation Mistral), `-llm-daily-tokens` (budget par jour UTC, défaut 1 000 000), `-llm-workers`, `-llm-base-url` (faux serveur de développement).
Le budget ne compte que les jetons de sortie demandés au maximum : il peut être légèrement dépassé.

**Mémoire et lore** : un PNJ peut demander au joueur de se présenter. La fiche (pseudo, rôle, origine, but, trait), déclarée par le joueur donc
non vérifiée, est mémorisée par lieu (8 lieux au plus par compte). Les autres joueurs l'entendent sous forme de **rumeurs** : visibles à
3 régions au plus, avec 5 minutes de délai par région, de moins en moins fidèles avec la distance, oubliées après 14 jours. Chaque PNJ garde aussi
un résumé **privé** de ses conversations avec chaque joueur, jamais montré aux autres. Le serveur y ajoute des faits vérifiés (niveau, boss tués, morts).

**Confidentialité** : un avis s'affiche à la première discussion et exige un accord explicite (une fois par compte). Sans accord, rien n'est envoyé au fournisseur.

**Modération et exploitation** : modération Mistral des entrées et des fiches (en cas d'échec, rien n'est écrit), limites par compte et file bornée.
`/stats` expose un bloc `llm`. Sur la base (serveur éteint) :
`server -lore-list [-lore-account NOM]` et `server -lore-purge account:NOM` ou `place:RX,RY`.

`make llm-smoke` fait un essai réel de l'API (ignoré sans clé).

## Serveur

```sh
./bin/sudo-dragon-server -addr :8080 -db /var/lib/sudo-dragon/game.db
```

| Option | Défaut | Rôle |
|---|---|---|
| `-addr` | `:8080` | adresse d'écoute |
| `-db` | `sudo-dragon.db` | base SQLite (comptes, personnages) |
| `-seed` | `1` | seed du monde, **fixée à la création de la base** |
| `-tls-cert`, `-tls-key` | | active TLS (`wss://`) |
| `-max-registrations` | `10` | créations de compte par heure et par adresse IP (`0` = illimité) |
| `-login-workers` | moitié des cœurs | hachages de mot de passe simultanés (voir *Performances*) |
| `-pprof` | désactivé | profilage Go, ex. `127.0.0.1:6060` (ne jamais l'exposer publiquement) |

**Données.** Tout l'état persistant tient dans le fichier SQLite (mode WAL : `.db`, `.db-wal`, `.db-shm`).
Le serveur sauvegarde chaque joueur à sa déconnexion, après chaque combat, toutes les 30 secondes et à l'arrêt
(`Ctrl+C` / `SIGTERM` : arrêt propre, sauvegardes vidées). Pour une copie de secours à chaud :
`sqlite3 game.db ".backup sauvegarde.db"`, ou arrêtez le serveur et copiez les trois fichiers.
Relancer avec une autre `-seed` sur une base existante est refusé, car les positions sauvegardées n'auraient plus de sens ;
utilisez une autre base (`-db`) pour un nouveau monde.

**Sécurité.**
- Les mots de passe sont hachés avec bcrypt ; un pseudo inconnu et un mauvais mot de passe donnent le même message.
- Quotas par adresse IP : 8 échecs de connexion par minute, 10 créations de compte par heure ; chat limité à une rafale de 5 messages puis 1 par seconde.
- **Le mot de passe voyage en clair sur `ws://`.** Hors de votre machine, utilisez `wss://` (option `-tls-*`, ou un proxy inverse).
- Derrière un proxy inverse, toutes les connexions semblent venir du proxy : les quotas par IP s'appliquent alors à l'ensemble des joueurs.

**Proxy inverse (exemple nginx)** :

```nginx
location /ws {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;
}
```

**Supervision.** `GET /healthz` répond `ok`. `GET /stats` renvoie du JSON : joueurs, monstres, nombre de ticks,
ticks lents (au-delà de 100 ms), durée moyenne/maximale d'un tick, et p99 et maximum sur les 60 dernières secondes.
Ces deux adresses ne sont pas authentifiées.

## Architecture

```
cmd/server      serveur de jeu            cmd/client    client TUI
cmd/bot         joueurs automatiques      cmd/worldview affiche une zone du monde en ASCII
internal/
  world         génération procédurale : terrain par chunks 32×32, lieux par régions 256×256 (déterministe d'après la seed)
  game          boucle de jeu à 10 Hz : joueurs, monstres, garnisons, combats, PNJ, instantanés
  combat        règles du tour par tour et progression (pur, aléa injecté)
  protocol      messages JSON et encodeur rapide des instantanés
  server        WebSocket, sessions, authentification, chat, quotas
  store         SQLite : comptes, personnages, écrivain asynchrone
  client        modèle Bubble Tea et connexion réseau
  render        rendu ASCII coloré, sprites de combat
```

- **Serveur autoritatif.** Le client n'envoie que des intentions (déplacement, action de combat, message) ; le serveur valide tout.
- **Une seule goroutine possède l'état du jeu** (`game.Run`) : pas de verrou sur l'état, les sessions lui passent des commandes.
- **Le client génère le terrain lui-même** à partir de la seed reçue à la connexion : les instantanés ne contiennent que les entités
  (joueurs, monstres) dans la zone de vision (±40 × ±20 cases), et ne sont envoyés que si leur contenu a changé.
- **Instantanés fusionnés.** Un client lent saute des images au lieu d'être déconnecté ; seuls les messages fiables (chat, combat)
  qui s'accumulent font fermer la connexion. L'encodage JSON se fait dans la goroutine d'écriture de chaque session.
- **Persistance asynchrone.** La boucle de jeu ne touche jamais le disque ; un écrivain regroupe les sauvegardes, et une reconnexion
  immédiate relit d'abord les sauvegardes encore en attente.

## Développement

```sh
make check        # format, analyse statique, tests avec détecteur de courses
make test         # tests seuls
make load N=250   # test de charge
```

Les tests couvrent les règles de combat, l'équilibrage, la génération du monde, le protocole (l'encodeur rapide est comparé octet
pour octet à `encoding/json`), l'authentification, les quotas, la persistance (y compris la reconnexion immédiate) et l'interface.

`go run ./cmd/worldview -seed 42 -x 0 -y 0 -w 120 -h 40` affiche une zone du monde sans lancer le jeu.

## Performances

`make load N=…` lance un serveur éphémère, crée `N` bots, les regroupe au point de départ (le pire cas : tous se voient)
et mesure la cadence réelle du serveur en régime établi. Mesures sur une machine à 8 cœurs, **bots et serveur sur la même machine**
(les bots consomment une grande part du CPU : un serveur dédié ferait mieux) :

| Joueurs regroupés | Cadence | Tick moyen | Tick p99 | Ticks lents | Trafic sortant |
|---|---|---|---|---|---|
| 100 | 10,0 /s | 2,7 à 3,2 ms | 8 à 23 ms | 0 | ~2,6 à 3 Mo/s |
| 250 | 10,0 /s | 6,7 ms | 85 ms | 0 | ~13 Mo/s |

Le budget d'un tick est de 100 ms. Le débit de **connexion** est borné volontairement : chaque connexion exige un hachage bcrypt
d'environ 60 ms de CPU, plafonné par `-login-workers` pour ne pas affamer la boucle de jeu. Avec 250 clients se connectant en
quelques secondes, certains attendent plusieurs secondes ; augmenter `-login-workers` accélère les connexions au prix de ticks plus lents.

## Limites connues et pistes

- **Le trafic croît comme le carré du nombre de joueurs dans une même zone** : chaque instantané contient toutes les entités visibles.
  Une codification en différences (ajouts, retraits, déplacements) et un index spatial des joueurs seraient la prochaine optimisation.
- **Un combat oppose un joueur à un monstre** ; pas de combat en groupe. Pas de classes, de quêtes, de PNJ, d'inventaire riche ni d'économie.
- **Le monde n'est pas modifiable** : il se régénère toujours à l'identique depuis la seed, donc ni chunk ni lieu n'est persisté.
- **Mondes existants** : la génération des lieux ne change pas la seed, mais des villages et des forteresses apparaissent là où il n'y avait que du terrain.
  Un personnage sauvegardé dans ce qui est devenu un mur est replacé sur la case libre la plus proche à sa connexion.
- **Le marchand ne vend que des potions** et il n'y a ni équipement, ni autre objet : l'or n'a donc qu'un usage, ce qui borne l'économie.
- Les garnisons de forteresse ne se retrouvent qu'en mémoire : un redémarrage du serveur les remet toutes en place, y compris le seigneur de guerre.
- **Pas de reconnexion automatique** : après une coupure, le client revient à l'écran de connexion.
- Un joueur qui coupe sa connexion en plein combat garde ses PV du moment sans subir la défaite éventuelle.
- Un seul processus serveur, sans montée en charge horizontale.
- Le serveur et la base sont en français (messages, journaux) ; il n'y a pas de couche de traduction.
