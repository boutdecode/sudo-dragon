# sudo Dragon

*[English version: README.en.md](README.en.md)*

Un RPG multijoueur en monde ouvert, à l'ancienne, entièrement en ASCII dans le terminal.
Interface avec [Bubble Tea](https://charm.land/) et Lip Gloss ; serveur autoritatif en Go.

```
╭───────────────────────────────────────────────╮╭────────────────────────╮
│"'"""""""""'"""""""'".,:~~~~~~~--~~~~~~~~~~~~~~~││hero                    │
│""""'""""""""'""".'"".""..:.~~~~~-~~~~~~~~~~~~~~~││Pos  4, 2               │
│"""""""."""""".""."."'""""""..~~~~~~~~~~~~~~~~~~~││Sol  Plaine             │
│""".""""""""""r"""""""""".'""",.~~-~~~~~~~~~~~~~~││Niv 1   XP 4/120        │
│""""""""'"'""."".""""'"'""'""".,.~~~~~~~~~~~~~~~~││PV ########## 30/30     │
│"""""""""""""""@""""""""""""""..~~--~~~~~~~~~~~~~││Déf 11 Dég 1d6 Pot 2    │
╰───────────────────────────────────────────────╯╰────────────────────────╯
* hero a rejoint le monde
Entrée: chat (/help) · flèches/ZQSD/hjkl: bouger · Ctrl+C: quitter
```

*(illustration schématique, les couleurs ne sont pas rendues ici)*

- **Monde infini** généré par bruit simplex à partir d'une seed : océans, continents, îles, rivières, chaînes de montagnes, et des biomes selon le climat
  (plages, plaines, forêts, collines, montagnes, neige, toundra, désert, marais, jungle). Cycle jour / nuit.
- **Villages, villes et forteresses** générés eux aussi depuis la seed : zones sûres, marchands, soigneurs et gardes dans les premiers, garnisons et seigneur de guerre dans les secondes ; **routes** entre lieux voisins et **caravanes** de marchands ambulants.
- **Donjons secrets**, **points d'intérêt** (feux de camp, sanctuaires, repaires, ruines…) et **cartes au trésor** à découvrir.
- **Multijoueur** : tout le monde partage le même monde, se voit, discute et se déplace en temps réel.
- **Monstres** par biome, de plus en plus forts loin du point de départ, qui errent et vous poursuivent ; **combat au tour par tour** (parfois en groupe) avec potions, fuite, XP et niveaux.
- **Personnage à six caractéristiques**, trois classes, **équipement** de cinq rangs acheté, trouvé ou arraché aux monstres.
- **Récolte** : bois, minerais, plantes et poissons, avec quatre métiers qui montent de niveau.
- **Or et boutiques**, **quêtes** et **réputation** ; des PNJ avec qui l'on peut discuter librement (modèle de langage, optionnel).
- **Comptes et sauvegarde** : pseudo et mot de passe (bcrypt), personnage persistant (SQLite).

La liste détaillée de ce qui est livré est dans [FEATURES.md](FEATURES.md).

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

Options du client : `-addr` (adresse du serveur), `-name` (pseudo prérempli), `-transparent`, `-lang`.
Pour jouer sur un autre serveur : `./bin/sudo-dragon -addr wss://jeu.exemple.org/ws -name monpseudo`.
`SUDO_DRAGON_NO_FX=1` désactive les secousses d'écran du combat.

### Langue

Le jeu existe en **français** et en **anglais**. Le client choisit la langue du système (variables `LC_ALL`, `LC_MESSAGES`,
`LANGUAGE`, puis `LANG`, comme `gettext`) : `fr_*` donne le français, `en_*` l'anglais, et toute autre langue l'anglais.
`-lang fr` ou `-lang en` impose une langue (`./bin/sudo-dragon -lang en`).

La langue est annoncée au serveur à la connexion et chaque joueur est servi dans la sienne, sur un même serveur : erreurs, aide du
chat, annonces d'arrivée, journal de combat, marchands, soigneurs, titres et traits des PNJ, et même la consigne donnée au modèle de
langage (le PNJ répond dans la langue du joueur). Les noms propres (lieux, prénoms) sont communs ; seuls les mots courants changent
(« Citadelle » devient « Citadel », « Sœur » devient « Sister »). Les raccourcis suivent les mnémoniques de la langue
(achat `A`/`B`, vente `V`/`S`, soin `S`/`H`, discussion `D`/`T`, accord `O`/`Y`, travail `T`/`W`). Les journaux et options du serveur restent en français.

Les textes sont dans `internal/i18n` (`fr.go`, `en.go`) ; un test vérifie que les deux catalogues ont les mêmes clés et les mêmes
directives de mise en forme. Pour ajouter une langue : un catalogue complet, enregistré dans `catalogs`, `Langs` et `Parse`.

## Jouer

**Se déplacer** : flèches, `Z Q S D` (AZERTY), `W A S D`, ou `h j k l`. Diagonales : `y u b n`.
Un joueur avance de 5 cases par seconde ; tous les monstres sont plus lents, on peut donc toujours leur échapper.

**Touches** :

| Touche | Écran |
|---|---|
| `Entrée` | chat |
| `I` | sac (équipement, potions, matières) |
| `P` | fiche de personnage |
| `M` | carte du monde |
| `Q` | livre de quêtes |
| `G` | récolter (ou, sur la carte, marcher jusqu'au curseur) |
| `Ctrl+C` | quitter |

**Chat** : `Entrée` ouvre la saisie, `Entrée` envoie, `Échap` annule. `PgHaut`/`PgBas` font défiler l'historique, `Ctrl+↑`/`Ctrl+↓` redimensionnent la zone de discussion,
`↑`/`↓` dans la saisie rappellent les dernières lignes envoyées.

| Saisie | Effet |
|---|---|
| `message` | chat **local** : seuls les joueurs proches (dans votre zone de vision) le voient |
| `/g message` | chat **global**, tout le monde |
| `/me action` | émote locale |
| `/w pseudo message` | message **privé** |
| `/goto x y` ou `/goto nom` | **marche automatique** vers des coordonnées, un repère, un lieu ou une entrée de donjon découverts (Tab complète le nom) |
| `/menhir [n° ou nom]` | voyage d'une pierre dressée activée à une autre (voir *Points d'intérêt*) |
| `/help` | rappel des commandes |

**Carte du monde (`M`)** : zoom (`+` / `-`), curseur aux flèches, `0` recentre ; seul le terrain exploré est affiché. `W` pose ou renomme un **repère** (9 au plus),
`X` le supprime, `G` lance la marche automatique vers le curseur. La carte, les repères et l'exploration sont enregistrés côté client.

**Cycle jour / nuit** : l'en-tête affiche l'heure de jeu (une heure de jeu = une minute réelle) et le terrain s'assombrit la nuit. Purement visuel.

### Combat

Marchez sur un monstre pour l'attaquer, ou laissez-le vous toucher. Les monstres libres à moins de 5 cases se joignent au combat : jusqu'à **3 monstres** par combat (2 pour une garnison ou un donjon).
Une **initiative** (d20 + AGI) décide qui ouvre : les monstres strictement plus rapides frappent avant votre premier tour.

L'écran de combat propose : `A` attaquer (frapper, tirer ou lancer un sort selon l'arme) · `I` boire une potion (+15 PV) · `F` fuir (une chance sur deux).
Les touches `1` à `3` font la même chose ; flèches haut/bas + Entrée choisissent l'action, gauche/droite la cible. Après la victoire, `Entrée` ferme l'écran.

- Vainqueur : de l'XP, de l'or, 30 % de chances de trouver une potion, parfois un objet d'équipement (voir *Équipement*). Chaque niveau lance le dé de vie de la classe (+ bonus de CON) et restaure tous les PV et la mana ; il ne change ni le toucher ni les dégâts, qui viennent des caractéristiques et de l'équipement.
- Vaincu : vous réapparaissez au dernier **feu de camp** utilisé (sinon au point de départ, ou devant l'entrée du donjon) avec tous vos PV, sans perte d'XP.
- Fuite : après une fuite, **aucun monstre ne vous engage pendant 5 secondes**.
- Les PV remontent d'un point toutes les 3 secondes hors combat.
- Pendant un combat, vous et vos adversaires restez sur place, hors d'atteinte des autres, et apparaissez en rouge pour les autres joueurs.
  Un joueur inactif depuis 60 secondes est considéré comme ayant fui.

**Règles** : chaque créature a une **DEF** (8 pour une petite créature, 10 pour un humanoïde ou une créature moyenne, 12 pour une grande, plus le modificateur d'AGI). Pour toucher, on lance un **d20** et on ajoute
son modificateur (FOR en mêlée, AGI à distance, INT en magie) : il faut faire **strictement plus** que la DEF de la cible. Un 20 naturel touche toujours et **double les dés** de dégâts (pas les bonus) ; un 1 naturel rate toujours.
Les dégâts sont ceux de l'arme (`x d y + z`) plus le modificateur de caractéristique, **moins les dés de réduction de l'armure** de la cible (au moins 1 si le coup touche). Le journal de combat montre le jet, la DEF visée et la réduction.
Sans mana, une arme magique sert de gourdin (1d4 + FOR).

### Monstres

| Monstre | Glyphe | Habitat | PV | DEF | Toucher | Dégâts | Réduction | XP | Détection |
|---|---|---|---|---|---|---|---|---|---|
| Rat | `r` | plaine | 8 | 8 | +1 | 1d4 | – | 4 | 5 cases |
| Crabe | `c` | plage | 12 | 8 | +2 | 1d6 | 1d4 | 6 | 3 cases |
| Loup | `w` | forêt | 16 | 11 | +3 | 1d6+1 | – | 10 | 9 cases |
| Gobelin | `g` | collines | 22 | 10 | +3 | 1d8 | – | 15 | 8 cases |
| Yéti | `Y` | neige | 34 | 12 | +5 | 2d6 | 1d4 | 30 | 7 cases |
| Scorpion | | désert | 14 | 8 | +3 | 1d6 | 1d4 | 12 | 4 cases |
| Panthère | | jungle | 24 | 12 | +4 | 1d8+1 | – | 20 | 9 cases |
| Crapaud | | marais | 14 | 8 | +2 | 1d6 | – | 8 | 4 cases |
| Ours | | toundra | 30 | 12 | +4 | 2d6 | 1d4 | 22 | 6 cases |
| Squelette | `S` | forteresse, donjon | 36 | 10 | +4 | 2d6 | 1d4 | 25 | 7 cases |
| Chevalier | `K` | forteresse, donjon | 90 | 12 | +7 | 2d8 | 1d8 | 50 | 7 cases |
| Seigneur de guerre | `B` | forteresse | 180 | 13 | +8 | 3d10+3 | 2d6 | 250 | 6 cases |
| Spectre | | donjon | 30 | 12 | +5 | 2d6 | 1d4 | 30 | 7 cases |
| Golem | | donjon | 110 | 11 | +6 | 2d10 | 1d10 | 60 | 5 cases |
| Liche | | donjon (boss) | 200 | 12 | +8 | 3d10+4 | 2d6 | 300 | 7 cases |

Ce sont les statistiques de base. Chaque monstre est **mis au niveau de sa région** à son apparition (voir *Niveau de région*) : PV et XP +25 % par niveau au-delà du premier, +1 au toucher tous les 3 niveaux, +1 aux dés de dégâts tous les 4 niveaux, +1 de DEF tous les 5 niveaux. Les monstres de donjon suivent le palier du donjon.
Les monstres sont parfois accompagnés de **compagnons** (rats avec les loups, loups avec les gobelins…) : un compagnon de plus tous les 3 niveaux du joueur.

Un monstre vaincu laisse de l'or (entre la moitié et la totalité de son XP). Un personnage débute niveau 1 avec ses PV de classe (voir « Caractéristiques et classes »), sa DEF de 10 + modificateur d'AGI, son **arme de départ** (épée courte du guerrier, arc court du gredin, bâton du magicien : 1d6), 2 potions et aucun or ; il faut `120 × niveau²` XP pour passer au niveau suivant (environ 60 h de jeu actif pour atteindre le niveau 20).
Un guerrier débutant bat un rat ou un loup, mais pas un yéti : il faut progresser et s'équiper (c'est vérifié par un test d'équilibrage ; le gredin et surtout le magicien, plus fragiles, évitent le loup).

## Le monde

### Niveau de région, régions et contrées

Le monde repose sur plusieurs découpages, tous calculés depuis la seed (le client les retrouve sans rien recevoir) :

- **La grille de régions** (carrés de 256 × 256 cases) est l'unité technique : au plus un lieu par région, rumeurs qui voyagent sur 1 région (le lieu et ses voisines), réputation par région.
- **Le niveau de région** (1 à 20) croît avec l'éloignement du point de départ (un niveau toutes les ~154 cases). Il règle la force des monstres, le rang de l'équipement vendu et lâché, et le palier des donjons. Le journal annonce le passage à une tranche plus dangereuse.
- **Les contrées** sont des pays nommés à frontières irrégulières (diagramme de Voronoi à germes décalés, environ 1 000 cases de large). Leur nom reflète le terrain dominant
  (« Forêts d'Aube », « Plaines de Solbrune », « Côtes de Kerhaven »…) et existe en français et en anglais. Les lieux d'une contrée ont un air de famille : leurs noms puisent dans la même
  petite famille de syllabes, sans jamais se répéter à moins de 3 régions. Chaque contrée a un **climat** (tempéré, tropical, aride, boréal) qui change les couleurs du terrain ; il est purement visuel.

L'interface affiche en haut de la carte `contrée · Niv. N · lieu · terrain (x, y)`, le lieu (« village Tourelle », « forteresse… ») n'apparaissant que dans l'emprise ou la zone sûre d'un village,
d'une ville ou d'une forteresse. Une ligne s'ajoute au journal en entrant dans une nouvelle contrée ou un nouveau lieu. Les PNJ connaissent la contrée de leur lieu.
`worldview -territories -step 100` dessine les contrées en lettres avec leur légende.

**Versions du générateur.** Une base garde la version de terrain avec laquelle elle a été créée : un monde ancien ne change jamais de relief.
La v1 est un terrain simple sans rivières ; la **v2** ajoute continents, rivières, chaînes de montagnes et biomes ; la **v3** (nouveaux mondes) ajoute les nœuds de ressources sans toucher au terrain de la v2.
`worldview -gen N` dessine une version donnée.

### Villages, villes et forteresses

Chaque région accueille **au plus un lieu** (environ 70 % en ont un avec le générateur v2),
choisi selon le terrain : les villages en plaine ou en forêt, les villes en plaine et de préférence près de l'eau, les forteresses sur les collines.
Un village se trouve toujours à quelques dizaines de cases du point de départ. Les lieux sont **calculés à l'identique par le serveur et par le client**
à partir de la seed : rien n'est envoyé ni sauvegardé. Deux lieux voisins sont reliés par une **route**, qui franchit les rivières par un pont.

| Lieu | Taille | Contenu |
|---|---|---|
| Village | ~32 × 26 | 4 à 8 maisons reliées à une place par des chemins, un **soigneur**, 3 **villageois**, un **chef**, un forgeron ou un armurier, 2 **gardes** |
| Ville | ~58 × 46 | muraille à quatre portes, deux avenues, une place, une vingtaine de bâtiments, un **marchand**, un **soigneur**, 5 **villageois**, un **maire**, un **forgeron**, un **armurier**, 5 **gardes** |
| Forteresse | 48 × 40 | enceinte épaisse à tours d'angle et un portail, cour, deux casernes, donjon cloisonné ; **8 soldats et le seigneur de guerre** |

Sur la carte : `#` mur, `+` porte, `:` chemin, `_` sol intérieur, `M` marchand, `H` soigneur, `F` forgeron, `R` armurier, `C` chef de village ou maire (il connaît les lieux à 3 régions alentour, direction et distance, et renseigne même sans clé Mistral), `V` villageois, `!` villageois qui confie des quêtes (sans métier : on ne fait que discuter avec eux ; sans clé Mistral ils disent une phrase d'ambiance), `G` garde, `E` caravane.

**Zones sûres.** Un village ou une ville, avec trois cases de marge, est une zone sûre : aucun monstre n'y apparaît ni n'y entre,
un joueur qui s'y trouve n'est ni poursuivi ni attaqué par les monstres, et il y regagne 1 PV par seconde (au lieu d'un toutes les 3 secondes).

**PNJ.** Marchez sur un PNJ pour lui parler (vous restez immobile pendant la conversation ; `Échap` pour la quitter, abandon automatique après 60 secondes).

| PNJ | Action | Prix |
|---|---|---|
| Marchand | `A` acheter une potion | 20 or |
| Marchand | `V` vendre une potion | +8 or |
| Marchand | faire les poches (voir *Vol*) | |
| Soigneur | `S` soins complets | gratuit au niveau 1, ensuite 4 or × niveau |
| Garde | où trouver un habitant ? lieux voisins ? | |

Le serveur décide des prix et des refus (or insuffisant, sac plein, déjà en pleine forme : rien n'est alors débité). Chaque transaction est sauvegardée aussitôt.

**Gardes.** Un village a 2 gardes, une ville 5 : ils patrouillent sur les chemins du lieu, même en zone sûre. Marcher sur un garde lui adresse la parole : il indique où trouver le soigneur, le marchand, le forgeron, l'armurier ou le chef, et les lieux voisins (sans clé Mistral).
Un joueur **Recherché** (voir *Réputation*) dans ce lieu est poursuivi et combattu par les gardes, même en zone sûre ; les tuer coûte de la réputation.

**Vol.** Le marchand propose « Faire les poches » : 50 % de chances, modulées par le charisme ; un succès rapporte quelques pièces, un échec coûte 3 points de réputation dans le lieu, et un marchand ne se laisse pas voler deux fois en 3 minutes.

**Caravanes.** Sur une route entre deux lieux voisins sur deux circule une caravane (`E`) : un forgeron ou armurier ambulant qui fait l'aller-retour. Elle vend de l'équipement d'un rang au-dessus des forges de sa région, avec un assortiment qui change chaque jour de jeu.

**Forteresses.** La garnison apparaît quand un joueur s'approche (à moins de 40 cases) et ne quitte jamais la forteresse : fuir suffit à lui échapper.
Chaque soldat occupe un poste ; vaincu, il n'est remplacé qu'au bout de **5 minutes** (15 pour le seigneur de guerre). S'éloigner ne déclenche aucun délai.
Le **seigneur de guerre**, au centre du donjon, laisse un trésor garanti : **100 or et 3 potions**. Ordre de grandeur, un joueur à l'aise contre
un squelette vers le niveau 4, un chevalier vers le 8, et le seigneur de guerre vers le 16 (ces seuils sont verrouillés par des tests d'équilibrage).

`go run ./cmd/worldview -seed 1 -sites -x 0 -y 0 -w 400 -h 0` liste les lieux d'une zone ; avec une hauteur non nulle, il les dessine
(`S` soldat, `K` élite, `B` seigneur) pour préparer une expédition.

### Donjons secrets

Environ une région sur cinq cache un **donjon** dont l'entrée est une case de terrain ordinaire : on la **découvre** en passant à 3 cases ou moins (pour ce personnage seulement), en rendant certaines quêtes, ou en explorant des ruines ou une tour de guet.
Une entrée découverte s'affiche `>` sur la carte et se rejoint par `/goto <nom>`. Chaque donjon a un nom unique, un **palier** de 1 à 5 (plus fort loin du départ) et un plan de salles et de couloirs tiré de sa propre seed.

Marcher sur l'entrée y emmène, avec les joueurs libres à moins de 5 cases ; tous les joueurs d'un donjon le partagent. Des gardes (squelettes, spectres), des élites (chevaliers, golems) et la **liche** (le boss) l'habitent ; des **coffres** donnent or, potions et équipement du palier,
et celui du boss, verrouillé tant que la liche vit, contient toujours un objet un rang au-dessus. On sort par l'escalier ; vaincu, on se réveille devant l'entrée.
Le donjon garde son état tant que la liche vit ; une fois qu'elle est tombée, il se reforme un jour de jeu plus tard. `M` y ouvre le plan déjà exploré.

### Points d'intérêt et cartes au trésor

De petits repères à une case, visibles d'emblée sur la carte, auxquels on touche en **marchant dessus** :

| Point | Glyphe | Effet |
|---|---|---|
| Feu de camp | `&` | soigne la moitié des PV et de la mana, et devient votre **point de réapparition** |
| Sanctuaire | `o` | bénédiction de 5 min selon le climat (DEF, régénération, toucher ou XP) |
| Repaire | `x` | trois gardiens à vaincre, puis un coffre (or, potions, équipement) |
| Tour de guet | `I` | révèle les entrées de donjon proches et explore 96 cases de carte |
| Pierre dressée | `\|` | s'active au premier passage ; `/menhir` téléporte vers une autre pierre activée contre de l'or |
| Source | `v` | rend des PV et de la mana (une oasis en climat aride) |
| Ruines | `/` | un gardien d'élite protège un coffre, un fragment de lore et l'entrée du donjon le plus proche |

Les coffres de repaire, de ruines et les monstres de la surface laissent parfois une **carte au trésor** : arriver exactement sur la case indiquée (`/goto x y`) déterre or, potions et équipement.
Le détail des effets, délais et valeurs est dans [FEATURES.md](FEATURES.md).

### Ressources et récolte

Dans les mondes en v3, des **nœuds de ressources** parsèment le terrain : arbres, filons, plantes (en plaine) et bancs de poissons (dans les rivières). Ils gardent le glyphe du terrain qu'ils recouvrent ; seule leur couleur (en gras) dit la ressource.
Touche **`G`**, sur la case du nœud ou à côté, pour tenter la récolte : un jet de réussite (40 % au niveau 1 du métier, jusqu'à 94 %), puis le nœud est épuisé pour tous pendant 10 minutes.
Quatre **métiers** (bûcheron, mineur, herboriste, pêcheur, niveau 1 à 10) progressent à chaque réussite et agrandissent le sac de la catégorie. Les ressources, 18 déclinaisons plus les poissons, dépendent du climat et du niveau de la région ;
elles vont dans un **sac de matières** (listé dans l'écran du sac `I`) et ne servent pas encore : l'artisanat viendra ensuite.

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
1 région au plus (le lieu et les régions adjacentes), avec 1 heure de délai pour passer à la région voisine, oubliées après 14 jours. Chaque PNJ garde aussi
un résumé **privé** de ses conversations avec chaque joueur, jamais montré aux autres : un PNJ qui n'a jamais parlé au joueur ne le connaît que par rumeurs et le traite en inconnu.
Le serveur y ajoute des faits vérifiés (niveau, boss tués, morts, quêtes rendues).

**Confidentialité** : un avis s'affiche à la première discussion et exige un accord explicite (une fois par compte). Sans accord, rien n'est envoyé au fournisseur.

**Modération et exploitation** : modération Mistral des entrées et des fiches (en cas d'échec, rien n'est écrit), limites par compte et file bornée.
`/stats` expose un bloc `llm`. Sur la base (serveur éteint) :
`server -lore-list [-lore-account NOM]` et `server -lore-purge account:NOM` ou `place:RX,RY`.

`make llm-smoke` fait un essai réel de l'API (ignoré sans clé).

## Caractéristiques et classes

À la première connexion, un écran de **création** propose six caractéristiques tirées par le serveur (**3d6**, soit 3 à 18) :

| | Rôle |
|---|---|
| FOR (force) | attaque et dégâts de **mêlée** |
| AGI (agilité) | attaque et dégâts **à distance** |
| INT (intelligence) | attaque et dégâts **magiques** |
| CON (constitution) | points de vie |
| SAG (sagesse) | mana |
| CHA (charisme) | perception par les PNJ : prix, et première impression donnée à leur modèle de langage |

Chaque valeur donne un **modificateur** : 3 → −4, 4-5 → −3, 6-7 → −2, 8-9 → −1, 10-11 → 0, 12-13 → +1, 14-15 → +2… (20-21 → +5).
Vous pouvez **relancer 3 fois en tout** (une caractéristique à la fois, touche `R`), puis choisissez une **classe** :

| Classe | Dé de vie | Mana de départ |
|---|---|---|
| Guerrier | d12 | 2 |
| Gredin | d8 | 4 |
| Magicien | d6 | 8 |

- **PV** : niveau 1 = dé maximal + modificateur de CON ; chaque niveau suivant = dé lancé + modificateur de CON (au moins 1). Les PV maximaux sont enregistrés.
- **Mana** = base de classe + 2 par niveau + niveau × modificateur de SAG. Un sort coûte 2 mana ; elle revient avec les PV (repos, soigneur, montée de niveau).
- **Style d'attaque = arme portée** : épée, hache… (mêlée, FOR) ; arc, arbalète (distance, AGI) ; bâton, baguette (magie, INT, 2 mana par sort). À mains nues : mêlée. Le modificateur s'ajoute au jet de toucher et aux dégâts.
- **Charisme** : ±2 % par point de modificateur sur les prix des PNJ (bornés à ±10 %, après la réduction de réputation).
- Les personnages créés avant cette version gardent niveau, or, quêtes et sac, mais repassent par la création à leur prochaine connexion ; leurs PV sont relancés niveau par niveau.
- La touche `P` ouvre la **fiche de personnage** : classe, niveau, PV, mana, attaque et défense (avec la part de l'équipement), style et chance de toucher, effet du charisme, or, position, palmarès, caractéristiques détaillées, équipement porté, bénédictions en cours, cartes au trésor, métiers de récolte et réputation. Un résumé des caractéristiques reste dans le panneau latéral.

## Équipement et forges

Quatre emplacements : **arme**, **armure**, **bouclier** et **casque**. Les objets ont un **palier** (= **rang**) de 1 à 5, qui est aussi le **niveau requis** (1, 5, 10, 15, 20).

- **Arme** : dégâts `x d y + z` avec **x = rang** (1 dé au rang 1, 2 au rang 2…), **y = famille** (type de dégâts) et **z = qualité** (0 jusqu'au rang). Familles : coupant d4 (dague, lance, baguette), tranchant d6 (épées, hache, arc court, bâton),
  contondant d8 (masse, arbalète), **deux mains** d12 (espadon, grande hache). L'arc court, l'arbalète et le bâton se portent à deux mains. Le style (mêlée, distance, magie) découle de la famille.
- **Armure** : bonus de DEF **et** dés de réduction (rang × d4 légère, d6 intermédiaire ou d8 lourde, retirés à chaque coup reçu). **Bouclier** et **casque** : bonus de DEF seulement.
- Une arme à **deux mains** interdit le bouclier (et inversement).

Le **sac** (touche `I`, 24 objets) garde tout ce qu'on achète ou trouve : `E` équipe ou retire l'objet choisi, un objet par emplacement, impossible en plein combat. Le panneau latéral, la fiche et le combat utilisent les valeurs effectives.
Un objet porté ne se revend pas ; la revente rapporte 40 % du prix. L'or est plafonné à 999 999.

**Butin** : les monstres lâchent parfois de l'équipement du rang de leur région (5 % pour un monstre de biome, 15 % pour un soldat de garnison, toujours pour le seigneur de guerre) ; les coffres de donjon, de repaire et de ruines, et les cartes au trésor en donnent aussi.

**Vendeurs** : chaque ville a un **forgeron** (armes, `F`) et un **armurier** (armures, boucliers, casques, `R`) ; chaque village n'a que l'un des deux ; les forteresses aucun. Achat par les chiffres `1`-`9`,
revente par `V`. La réputation du lieu réduit aussi les prix de l'équipement. Le **rang vendu dépend de la région** : un forgeron lointain ne vend plus d'équipement de débutant.

**Spécialité et étalage générés** : à la première visite, le modèle de langage invente en JSON la spécialité du vendeur et 6 à 8 objets (noms, descriptions, palier, **famille**, qualité, bonus de DEF, prix). **Le serveur décide** : emplacement
vendu, palier borné d'après la région, famille parmi celles de l'emplacement (les dés ne sont jamais choisis par le modèle), qualité, DEF et prix ramenés dans des bornes calculées d'après le palier, textes nettoyés et modérés, au moins 4 objets valides sinon l'étalage est écarté. L'étalage
est enregistré (`npc_wares`) et identique pour tous les joueurs ; aucun texte de joueur n'entre dans cette demande. Sans clé Mistral, ou tant que l'étalage n'est pas prêt, un **assortiment de repli**
déterministe est proposé : le jeu est complet sans IA. Un objet acheté est copié en entier dans le sac et ne change plus.

Côté opérateur : `server -wares-list` affiche les étalages générés, `server -wares-reset RX,RY,RANG` efface celui d'un vendeur pour qu'il soit régénéré. `/stats` compte étalages générés et rejetés.

## Quêtes et réputation

Certains villageois (marqués d'un `!` sur la carte ; au moins un par village ou ville) confient des **quêtes**. Il faut les dialogues IA (voir ci-dessus) pour
en recevoir de nouvelles, sauf les quêtes de donjon, proposées sans clé ; celles en cours progressent et se rendent sans clé.

**Déroulement** : en discutant (« je cherche du travail »), le PNJ peut proposer une quête. Le modèle de langage la formalise en JSON (titre, histoire, objectif, récompense), mais
**c'est le serveur qui décide** : il n'accepte que des objectifs qu'il sait vérifier lui-même, ramène la récompense dans des plafonds calculés d'après la difficulté, nettoie et modère les
textes, puis présente une carte **Accepter / Refuser** (`A` / `R`). Rien n'existe tant que le joueur n'a pas accepté. Le modèle ne juge jamais qu'une quête est accomplie.

- **Objectifs** : vaincre 3 à 12 monstres d'une espèce (seules comptent les victoires *après* l'acceptation), entrer dans un lieu voisin (à 3 régions au plus), vaincre le seigneur de guerre
  (niveau 10 au moins, forteresse à portée), ou vaincre la **liche d'un donjon secret** voisin (niveau requis selon le palier ; accepter la quête en révèle l'entrée).
- **Récompense** : or ≤ 2 × l'expérience totale des monstres, XP ≤ 25 %, potions ≤ 2 (3 pour le seigneur) ; le texte de remerciement n'a aucun effet.
- **Limites** : 5 quêtes ouvertes au plus, une par donneur, 30 minutes avant une nouvelle quête du même donneur, expiration au bout de 7 jours, abandon possible. Un donneur qui n'a rien à confier perd son `!`.
- **Livre de quêtes** (`Q`) : quêtes en cours avec progression et récompense réelle, onglet réputation et historique (`Tab`), abandon avec confirmation (`X` deux fois).
- **Remise** : on rend la quête au donneur (touches `1`, `2`… dans son menu). Or, XP, potions et réputation sont versés d'un coup et sauvegardés aussitôt.

**Réputation** : chaque quête rendue rapporte 2 à 4 points dans le lieu du donneur. Paliers : Recherché (≤ −10), Suspect (≤ −5), Inconnu, Connu (3), Estimé (8), Honoré (15). Aux paliers Estimé et Honoré, les soins, les potions
et l'équipement y coûtent 20 % puis 40 % de moins. Elle **baisse** de 1 en abandonnant une quête, de 3 en se faisant prendre à voler et de 5 en tuant un garde, avec un minimum de −20. Les PNJ du lieu connaissent la réputation du joueur et ses quêtes rendues ; la rumeur de ces exploits voyage comme celle des présentations
(1 région, 1 heure pour passer à la région voisine, oubli après 14 jours). Les quêtes rendues sont des faits établis par le serveur, pas des paroles de joueur.

Côté opérateur, `-lore-list` affiche aussi les quêtes rendues et `-lore-purge` supprime quêtes et réputation avec le reste. Les quêtes de `quests` et la réputation de `reputation` sont dans la base SQLite.

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
| `-llm`, `-llm-*` | `auto` | dialogues de PNJ par modèle de langage (voir *PNJ conversationnels*) |
| `-lore-list`, `-lore-purge`, `-wares-list`, `-wares-reset` | | outils d'administration sur la base, puis quitte |

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
  world         génération procédurale : terrain par chunks 32×32, lieux, routes, donjons, points d'intérêt (déterministe d'après la seed)
  resource      catalogue des ressources et métiers de récolte
  game          boucle de jeu à 10 Hz : joueurs, monstres, garnisons, gardes, donjons, combats, PNJ, instantanés
  combat        règles du tour par tour et progression (pur, aléa injecté)
  dice          dés et jets
  hero          caractéristiques, classes, modificateurs
  gear          équipement, étalages de repli, butin
  quest, lore   quêtes, réputation, mémoire et rumeurs des PNJ
  llm, npcai    client Mistral, modération, dialogues et génération d'étalages
  textsafe      nettoyage des textes issus du modèle
  protocol      messages JSON et encodeur rapide des instantanés
  server        WebSocket, sessions, authentification, chat, quotas
  store         SQLite : comptes, personnages, écrivain asynchrone
  client        modèle Bubble Tea et connexion réseau
  ui, render    composants d'interface, rendu ASCII coloré, sprites de combat
  i18n          langue du système, catalogues français et anglais
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

`go run ./cmd/worldview -seed 42 -x 0 -y 0 -w 120 -h 40` affiche une zone du monde sans lancer le jeu (`-gen`, `-raw -zoom N`, `-sites`, `-territories` pour d'autres vues).

La liste des fonctionnalités livrées est dans [FEATURES.md](FEATURES.md) ; elle se met à jour avec chaque changement de jeu.

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
*(Ces mesures datent d'avant les gardes, les donjons et les points d'intérêt.)*

## Limites connues et pistes

- **Le trafic croît comme le carré du nombre de joueurs dans une même zone** : chaque instantané contient toutes les entités visibles.
  Une codification en différences (ajouts, retraits, déplacements) et un index spatial des joueurs seraient la prochaine optimisation.
- **Pas de compétences ni de sorts à apprendre** : la magie est un style d'attaque lié à l'arme. Les ressources récoltées ne servent pas encore (pas d'artisanat).
- **Le monde n'est pas modifiable** : il se régénère toujours à l'identique depuis la seed, donc ni chunk ni lieu n'est persisté.
- **Mondes existants** : la version du générateur est fixée à la création de la base ; en revanche, des lieux ou des donjons ajoutés par une évolution du code peuvent apparaître là où il n'y avait que du terrain.
  Un personnage sauvegardé dans ce qui est devenu un mur est replacé sur la case libre la plus proche à sa connexion.
- **État en mémoire seulement** : garnisons de forteresse, gardes, donjons (monstres vaincus, coffres ouverts), repaires, cooldowns des points d'intérêt et nœuds de ressources épuisés ne sont pas sauvegardés ; un redémarrage du serveur remet tout en place, y compris le seigneur de guerre et les liches.
- **Les donjons sont partagés par tous** (pas d'instance privée par groupe) et un serveur qui tourne longtemps garde en mémoire ceux qui ne sont pas terminés.
- **Un combat oppose un joueur à des monstres** ; pas de combat en groupe entre joueurs.
- **Pas de reconnexion automatique** : après une coupure, le client revient à l'écran de connexion.
- Un joueur qui coupe sa connexion en plein combat garde ses PV du moment sans subir la défaite éventuelle.
- Un seul processus serveur, sans montée en charge horizontale.
- Seuls les journaux et les options du serveur sont en français ; les messages envoyés aux joueurs sont traduits (voir « Langue »).
