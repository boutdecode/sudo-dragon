# Fonctionnalités de sudo Dragon

Inventaire des fonctionnalités du jeu (anciennement « ASCII Adventure »), établi d'après le code. Pour l'installation, les options du serveur et l'exploitation, voir le [README](README.md).

## Monde et génération (`internal/world/`)

- **Monde infini** généré par bruit simplex depuis une seed, par chunks de 32×32.
- **Versions du générateur** (`World.GenVersion`) : un monde garde la version de terrain avec laquelle il a été créé (clé `gen` de la table `meta` ; une base sans cette clé est en **v1**, un nouveau monde en **v2**). Le serveur annonce la version dans le message `welcome` (`gen`, absent = 1) et le client construit le même terrain. La v1 est figée (`layout_golden_test`), la v2 l'est aussi.
  - **v1** : une élévation et une humidité à une seule échelle (lacs, mers et reliefs en patchwork), sans rivières.
  - **v2** (`world/terrain2.go`) : **continents** et **océans** (~600 cases, côtes déformées), **îles et archipels** en mer peu profonde, un continent garanti autour de l'origine, **chaînes de montagnes** (crêtes linéaires interrompues, bordées de collines), **rivières** qui serpentent (deux échelles de méandres, largeur de 2 à 5 cases mesurée à la distance de la ligne, variable le long du cours), naissent et meurent, et s'élargissent vers les côtes ; les chaînes de montagnes sont déformées de même, **lacs**, et un **climat** (température en bandes de latitude et en altitude, humidité à deux échelles et plus forte près de la mer, adouci autour de l'origine) qui donne les **biomes**. Les échelles sont resserrées pour que le paysage change toutes les 1 à 2 minutes de marche, et chaque biome contient ses **motifs locaux** (bosquets et clairières de 10 à 20 cases qui déchiquettent les lisières entre forêt et plaine, dunes, mares, taches de neige). La neige couvre les sommets des régions froides ; la plaine très froide est de la neige, puis de la toundra.
- **Terrains** : eau profonde, eau, sable, plaine, forêt, collines, montagne, neige ; en v2, aussi **rivière** (`-`, praticable : un gué, et un pont quand une route la traverse), **toundra** (`,`), **désert** (`;`), **marais** (`m`) et **jungle** (`J`), tous praticables. Tuiles de structures : chemin, sol intérieur, mur, porte. Tuiles de donjon : coffre, escalier. Tuiles de points d'intérêt : feu de camp, sanctuaire, tour de guet, pierre dressée, coffre de repaire, source, ruines (voir plus bas).
- **Lieux** : au plus un par région de 256×256 (en v1 55 % des régions ont un lieu, 70 % en v2 ; les régions en partie sous l'eau en ont moins).
  - **Village** (~32×26), **ville** (~58×46, avec muraille), **forteresse** (48×40).
  - Les plans sont recalculés à l'identique côté client et serveur : rien n'est envoyé ni stocké.
- **Niveau de région** (`world/level.go`) : il croît avec l'éloignement de l'origine (0, 0), en distance de Chebyshev, de 1 à 20 : un niveau toutes les ~154 tuiles, soit quatre **tranches** de cinq niveaux (1-5, 6-10, 11-15, 16-20) larges de 3 régions (768 tuiles). Il ne dépend pas de la graine, donc le client le recalcule sans message. Il règle la force des monstres, le rang de l'équipement vendu et lâché, et le palier des donjons.
- **Contrées** : pays nommés aux frontières irrégulières (Voronoi), noms selon le terrain dominant (côtes, plaines, forêts, collines, monts, et en v2 déserts, jungles, marais, toundras), en français et en anglais.
- **Climats** (`world/climate.go`) : chaque contrée a un climat : en v1 tiré de la graine (40 % **tempéré**, 20 % **tropical**, 20 % **aride**, 20 % **boréal**) ; en v2 déduit de la température et de l'humidité réelles moyennes de ses terres (chaud et humide : tropical, chaud et sec : aride, froid : boréal, sinon tempéré), pour que les couleurs suivent les biomes. Il est purement visuel (terrain, monstres et génération ne changent pas) et se recalcule des deux côtés sans message : franchir la frontière d'une contrée change les couleurs et les glyphes du terrain (tropical : verts vifs et mers turquoise ; aride : plaines jaunes, forêts olive clairsemées, collines rouges ; boréal : verts froids et bleutés). La neige, les routes des lieux, les structures et les donjons gardent leurs couleurs.
- **Routes entre lieux** (`roads.go`) : deux régions voisines (côte à côte, pas en diagonale) qui ont chacune un lieu sont reliées par une route.
  - Tracé par A* sur le terrain brut, confiné aux deux régions, donc identique quel que soit l'ordre de génération.
  - Les routes s'arrêtent au bord d'un lieu et rejoignent son chemin intérieur ; elles ne touchent jamais son emprise.
  - Pont sur les rivières (tuile `Road`) ; jamais d'eau profonde ni de montagne, et en v2 jamais d'eau du tout (mer, lacs, mares : les routes les contournent, des ports viendront plus tard) ; en v1 les routes gardent leur pont sur l'eau peu profonde. Coût croissant selon le terrain (plaine < forêt < collines < neige ; en v2 toundra et désert 2, marais 3, jungle 4, rivière 5).
- **Donjons** (`dungeon.go`, `dungeonmap.go`) : environ une région sur cinq cache un donjon, dont l'**entrée secrète** est une case de terrain ordinaire (hors lieux et eau ; une route peut la croiser).
  - **Nom unique** dans un rayon de 3 régions (« Crypte de Valmor »…), en français et en anglais ; identifiant stable `x,y` de la région.
  - **Palier** de 1 à 5, tiré du niveau de région au centre de la région du donjon (1 + niveau / 4, soit un de plus toutes les 4 niveaux) ; il règle le nombre de salles (6 ou 7 au palier 1, jusqu'à 11 au palier 5), de gardes et d'élites.
  - **Plan** (64×40) tiré de la **seed du donjon** : des salles reliées par des couloirs, escalier d'entrée dans la première, **boss** et son **coffre** dans la dernière, gardes et coffres entre les deux. Recalculé à l'identique côté client et serveur.
  - **Instance partagée** (`game/dungeon.go`) : chaque donjon a une seule instance, avec ses propres index de joueurs et de monstres (coordonnées locales au plan, donc deux donjons ne se heurtent jamais). Marcher sur l'entrée y emmène, avec les joueurs libres à moins de 5 cases ; tous les joueurs d'un même donjon se retrouvent dedans.
  - **Le donjon garde son état tant que son boss vit** : monstres vaincus et coffres ouverts le restent, qu'on sorte, qu'on meure ou qu'on se déconnecte, même quand il est vide (il est alors figé, sans calcul). Il **repart à neuf quand quelqu'un tue la liche, mais un jour de jeu plus tard** (24 heures de jeu, soit 24 minutes réelles ; une heure de jeu = 600 ticks, voir `protocol.TicksPerGameHour`), pour qu'on n'enchaîne pas les donjons.
  - Pendant ce délai, le donjon reste tel que la victoire l'a laissé, pour tous : boss mort, coffres vidés, rien ne revient, même s'il se vide. À l'entrée, le journal annonce « le donjon ne se reformera pas avant N h de jeu » (message `zone`, champ `reset`, arrondi à l'heure supérieure).
  - L'échéance passée, le premier visiteur trouve un donjon complet, boss et coffres compris, tandis que ceux qui étaient restés dans l'ancien finissent tranquillement dedans ; l'ancien se ferme avec son dernier joueur.
  - Un donjon dont la liche vient de tomber n'est pas confié en quête (elle ne pourrait pas être vaincue avant son retour).
  - L'état n'est qu'en mémoire : un redémarrage du serveur refait tout à neuf, délai compris.
  - Le donjon est peuplé à l'ouverture d'après les emplacements du plan : **gardes** (squelettes et spectres), **élites** (chevaliers et golems) et la **liche**, le boss. Aucun monstre de biome ni de garnison n'y apparaît, et rien ne repousse.
  - **Mise à l'échelle** : les monstres ont 80 % (palier 1) à 160 % (palier 5) de leurs PV et de leur XP, et un point de toucher de plus tous les deux paliers. Les combats de groupe y rassemblent 2 monstres au plus.
  - **Coffres** : on les ouvre en marchant dessus, une fois chacun tant que la liche vit (le premier arrivé les vide ; ils se refont avec le donjon). Ils donnent de l'or (15 à 30 par palier), parfois des potions (40 %) et, dans 35 % des cas, une pièce d'équipement du palier du donjon.
  - **Coffre du boss** : verrouillé tant que la liche vit, il contient toujours un objet, un rang au-dessus du palier (5 au plus). La liche laisse en plus 100 or + 40 par palier et 3 potions.
  - Un sac plein laisse le coffre fermé : on peut revenir. Le butin est fabriqué sans modèle de langage (`gear.DungeonLoot`), au maximum des bonus permis ; il n'a pas de vendeur d'origine (description « Trouvé au fond d'un donjon »).
  - Tuer la liche ne valide pas une quête de « seigneur de guerre ».
  - On sort par l'**escalier** ; vaincu, on se réveille devant l'entrée (le donjon garde son état). Déconnecté sous terre, on est sauvegardé devant l'entrée.
  - **Côté client** (`client/dungeon.go`) : le plan est recalculé d'après la seed du message `zone`, comme le monde. Seules les cases à moins de 7 cases du joueur ont été vues (le reste est de la roche, tuile `Rock` dessinée en `#` gris sombre ; seule la première couche de roche, au contact d'une salle ou d'un couloir diagonales comprises, reste en `#` blanc pour souligner les murs) ; un coffre ouvert apparaît vide. Le titre du panneau donne le nom du donjon et les coordonnées du plan ; `M` ouvre le plan déjà exploré avec le palier, et se referme avec `M` ou Échap.
  - Les donjons sont activés sur le serveur (`Game.Dungeons`, désactivé par défaut dans `game.New`, comme dans les tests).
  - **Découverte** : passer à 3 cases ou moins d'une entrée la révèle, pour ce personnage seulement. Le serveur le dit au joueur (« Vous découvrez l'entrée secrète de… »), mémorise la découverte (table `discoveries`, sauvegardée tout de suite) et la renvoie à chaque connexion. Entrer dans un donjon révèle aussi l'entrée à tout le groupe.
  - Une entrée découverte s'affiche `>` sur la carte principale et sur la carte du monde (`M`), figure dans la légende et dans la liste des lieux proches (avec le palier), et se rejoint par `/goto <nom ou début du nom>` : la marche s'arrête devant l'entrée, sans y entrer. Une entrée non découverte reste une case de terrain ordinaire.
  - Les quêtes de donjon (voir *PNJ, dialogues et quêtes*) révèlent aussi l'entrée du donjon visé, dès l'acceptation.
- **Points d'intérêt** (`world/poi.go`, `game/poi.go`) : de petits repères en plein air, **une case chacun**, au plus un par région en v1 (~78 % des régions) ; la v2 en ajoute jusqu'à **deux de plus** par région (55 % puis 30 %, à 40 cases au moins les uns des autres, jamais des pierres dressées : le réseau de pierres reste d'une par région), soit environ le double de points au total. Leur état (délais des feux, sources et sanctuaires ; repaires vidés) est tenu par point (région et indice). Position, type et nom (« Menhir de Valmor »…, en français et en anglais) viennent de la seed par des flux dédiés : le client les recalcule sans message et aucun lieu ni donjon existant ne bouge. Ils sont **visibles d'emblée** (pas de découverte) ; on les place sur du terrain praticable, loin des lieux, du point de départ et des entrées de donjon, et les routes les contournent. Ils s'affichent sur la carte principale et sur la carte du monde (`M`, une fois la zone explorée), avec leur ligne de légende. On interagit en **marchant sur la case** :

  | Point | Glyphe | Effet |
  |---|---|---|
  | **Feu de camp** (30 %) | `&` | Soigne la moitié des PV et du mana (une fois par 5 min et par feu) et en fait votre **point de réapparition** : après une défaite, on se réveille près du dernier feu allumé au lieu du point de départ (sauvegardé avec le personnage ; message dédié). Un donjon renvoie toujours devant son entrée. |
  | **Sanctuaire** (20 %) | `o` | Bonus de 5 min selon le **climat** de la contrée : tempéré +2 DEF, tropical +1 PV/s hors combat, aride +2 au toucher, boréal +50 % d'XP. Cooldown de 10 min par sanctuaire ; le bonus n'est pas sauvegardé et figure sur la fiche de personnage (`P`). |
  | **Repaire** (20 %) | `x` (coffre) | Trois **gardiens** (bêtes du biome, 2 niveaux de région de plus) restent à moins de 6 cases de leur poste ; activés à moins de 40 cases d'un joueur, repoussent 30 min après leur défaite. Le **coffre** reste fermé tant qu'un gardien vit, puis donne de l'or, 1-2 potions et, une fois sur deux, un équipement (rang de la région + 1) ; il se remplit de nouveau 30 min après son ouverture. Sac plein : le coffre reste fermé. État en mémoire. |
  | **Tour de guet** (15 %) | `I` | Révèle les entrées de donjon à 12 cases ou moins (comme une découverte) et, côté client, **marque comme exploré** un rayon de 96 cases sur la carte du monde. |
  | **Pierre dressée** (15 %) | `\|` | S'**active** au premier passage (sauvegardé). Debout sur une pierre activée, `/menhir` liste les autres pierres activées avec leur prix, `/menhir <n° ou nom>` y téléporte contre de l'or (15 or par région de distance, 20 au minimum). Impossible en combat ou en conversation. |
  | **Source** (~18 % des régions) | `v` | Rend 30 % des PV et 50 % du mana, avec 2 min de délai par source et par joueur (le délai ne court que si l'on a bu). En climat **aride**, c'est une **oasis** : 60 % des PV et tout le mana. Ne change pas le point de réapparition. |
  | **Ruines** (~8 % des régions) | `/` | Un **gardien** d'élite (bête du biome, 3 niveaux de région de plus, repousse en 30 min) protège le coffre. Une fois le gardien vaincu, le coffre donne or (×1,5 d'un repaire), 1-2 potions et, 4 fois sur 5, un équipement (rang de la région + 1) ; il se remplit de nouveau 30 min après son ouverture. Il livre aussi un **fragment de lore** (texte statique, sept variantes) et révèle l'**entrée du donjon le plus proche** (3 régions au plus) si on ne la connaît pas déjà. |
- **Cartes au trésor** (`game/treasure.go`, `world/dig.go`) : une carte retient la **case à creuser**, une case ordinaire de terrain praticable, à 40 cases au moins de l'endroit où on l'a trouvée et dans une région à 2 régions au plus. On en trouve dans le coffre d'une ruine (60 %) ou d'un repaire (25 %), et sur les monstres de la surface (2 %, 6 % pour un chevalier ou un boss). On en porte 5 au plus ; le journal donne les coordonnées (`/goto x y` pour s'y rendre) et la fiche de personnage (`C`) liste les cartes avec la distance. **Arriver exactement sur la case** déterre le trésor : or (25 × niveau de région, jusqu'au double), 1-2 potions et un équipement du rang de la région + 1. Sac plein : rien n'est creusé, la carte reste. Les cartes sont sauvegardées avec le personnage (colonne `maps`, migration 18).
- **Zones sûres** : village ou ville plus 3 cases de marge. Pas de monstres, pas de poursuite, régénération de 1 PV par seconde.

## Déplacement et exploration

- **Touches** : flèches, `Z Q S D`, `W A S D`, `h j k l`, diagonales `y u b n`.
- **Cadence** : 5 cases par seconde ; les monstres sont plus lents.
- **Vision** : ±40×±20 cases ; le serveur n'envoie que les entités qui ont changé.
- **Marche automatique `/goto`** (`client/goto.go`, `client/pathfind.go`) :
  - `/goto x y`, ou par nom de repère ou de lieu découvert (casse ignorée, ambiguïtés signalées).
  - **Complétion par Tab** dans le chat : après `/goto `, Tab complète le nom (repères, lieux découverts avec ou sans type, entrées de donjon ; sous terre `escalier`/`coffre`). Plusieurs noms possibles : complète le préfixe commun, puis liste les choix au Tab suivant.
  - Sous terre : par `escalier` ou `coffre` (le plus proche déjà vu, pas encore ouvert), ou par coordonnées du plan. La marche automatique ne traverse jamais un escalier de donjon ni une entrée de donjon, sauf si c'est la destination.
  - Recalcul du chemin après 4 ticks sans progrès, abandon après 8.
  - Interrompue par une direction, Échap, `q`, `i`, `p` ou `m`. Le serveur reste juge des collisions.
- **Carte du monde (`M`)** (`client/worldmap.go`) :
  - Zoom 1, 2, 4, 8 ou 16 tuiles par cellule (`+` / `-`), curseur clignotant aux flèches, `0` ou espace pour recentrer.
  - Les couleurs du terrain suivent le climat de chaque contrée (palette partagée avec la vue de jeu, `render.TileHex`) ; le titre du panneau donne la contrée et son climat, et la légende prend les couleurs du climat sous le curseur.
  - Seul le terrain exploré est affiché ; le reste est « inexploré » (`░`).
  - Légende (écran ≥ 90 colonnes) : vous, joueurs, village, forteresse, donjon, points d'intérêt (feu de camp, sanctuaire, repaire, tour, pierre), forêt, montagne, eau, route, repère, inexploré ; liste des repères, lieux et donjons découverts proches avec direction et distance.
- **Aller au curseur** : `G` lance la marche automatique (`/goto`) vers la case du curseur (case libre la plus proche) et ferme la carte.
- **Repères** : `W` pose ou renomme (24 caractères, noms uniques, 9 au plus), `X` supprime.
- **Localisation** : l'en-tête affiche `contrée · Niv. N · lieu · terrain (x, y)` (le niveau de région saute avec le reste quand la place manque) ; le journal annonce chaque nouvelle contrée (avec son climat) ou nouveau lieu, et le passage à une autre tranche de niveaux (« les créatures se font plus dangereuses »).

## Monstres (`internal/game/monsters.go`, `garrison.go`)

| Origine | Monstres |
|---|---|
| Biomes | rat (plaine), crabe (plage), loup (forêt), gobelin (collines), yéti (neige) ; en v2 aussi scorpion (désert), panthère (jungle), crapaud (marais), ours (toundra) |
| Forteresses | squelette, chevalier, seigneur de guerre (boss) |
| Donjons | squelette, chevalier, **spectre**, **golem**, **liche** (boss) |

- Errance et poursuite à portée de détection ; 500 monstres au plus, avec un quota par joueur.
- **Mise à l'échelle par région** (`scaledForLevel`) : un monstre de biome ou de garnison est mis au niveau de sa région à l'apparition : PV et XP +25 % par niveau au-delà du premier (×5,75 au niveau 20), +1 toucher tous les 3 niveaux, +1 aux dés de dégâts tous les 4 niveaux, +1 de DEF tous les 5 niveaux. Au niveau 1, les stats de base. L'XP et l'or de la victoire suivent. Les monstres de donjon suivent le palier du donjon.
- **Meutes** : un compagnon de plus tous les 3 niveaux du joueur ; des espèces compagnes apparaissent rarement.
- **Garnisons** : apparaissent à moins de 40 cases d'un joueur et restent dans la forteresse. Soldats : réapparition en 5 min ; seigneur : 15 min. Le seigneur laisse un trésor garanti (100 or, 3 potions).

## Combat (`internal/combat/`, `game/fight.go`, `client/combatview.go`)

- **Règles** : d20 + modificateur doit dépasser strictement la DEF. 20 naturel : touche et double les dés de dégâts ; 1 naturel : rate. La réduction d'armure est retirée des dégâts (minimum 1 si touché).
- **Groupes** : jusqu'à 3 monstres (2 pour les garnisons) ; les monstres libres compatibles à **5 cases** se joignent, même en marche ; ceux qui viennent de fuir ou de perdre un combat (2 s) restent à l'écart. Doublons numérotés (« Loup 1 », « Loup 2 »).
- **Initiative** : d20 + AGI. Les monstres strictement plus rapides frappent avant le premier tour ; à égalité, le joueur commence.
- **Actions** : Attaquer (`A`/`1`), Objet (`I`/`2`, potion +15 PV), Fuir (`F`/`3`, une chance sur deux). Flèches haut/bas + Entrée, gauche/droite pour changer de cible.
- **Interface** : deux colonnes (joueur / ennemis), détail de l'action, journal.
- **Illustrations** : rat, loup et crabe, en demi-blocs `▀` 24 bits (`render/image.go`). Les autres espèces n'en ont pas.
- **Secousse d'écran** (`client/fx.go`) : zone entière quand on est touché, colonne ennemie quand on touche ; 6 images de 60 ms (9 sur critique) ; PV qui clignotent en rouge. Désactivable avec `SUDO_DRAGON_NO_FX`.
- **Issue** :
  - Victoire : XP, or (moitié à totalité de l'XP du monstre), 30 % de chances d'une potion, et un **objet d'équipement** du rang de la région (5 % pour un monstre de biome, 15 % pour un soldat de garnison, toujours pour le seigneur ; un cran de plus pour un chevalier ou un seigneur). Le butin va dans le sac (rien s'il est plein) ; il est fabriqué comme celui des donjons (`gear.MonsterLoot`, description « Arraché à la dépouille d'un monstre »). Les monstres de donjon n'en lâchent pas : ils ont leurs coffres.
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

- **Niveaux** : `120 × niveau²` XP (≈ 300 000 XP cumulés jusqu’au niveau 20, soit ~60 h de jeu actif). À chaque niveau : dé de vie + modificateur de CON, PV et mana restaurés.
- **Mana** : base de classe + 2 par niveau + niveau × modificateur de SAG ; un sort coûte 2 mana.
- **Style d'attaque** déduit de l'arme : mêlée (FOR), distance (AGI), magie (INT). Sans mana, une arme magique sert de gourdin.
- **Charisme** : ±2 % par point de modificateur sur les prix, borné à ±10 %.
- **Fiche (`P`)** : classe, niveau, PV, mana, attaque, défense, or, position, palmarès (victoires, boss, morts), caractéristiques, équipement, **bénédictions de sanctuaire** en cours (bonus et temps restant, qui décompte côté client et disparaît à l'échéance ; la DEF affichée les inclut), **cartes au trésor** détenues, réputation.
- Les anciens personnages repassent par la création à leur prochaine connexion.

## Inventaire, équipement et commerce (`internal/gear/`, `client/bag.go`, `game/shop.go`)

- **Emplacements** : arme, armure, bouclier, casque. **Rangs** 1 à 5 (niveau requis 1, 5, 10, 15, 20).
- **Armes** : dégâts `rang d famille + qualité` (coupant d4, tranchant d6, contondant d8, deux mains d12). Arc court, arbalète et bâton se portent à deux mains.
- **Armures** : bonus de DEF et dés de réduction (d4 légère, d6 intermédiaire, d8 lourde). Bouclier et casque : DEF seulement.
- Une arme à deux mains interdit le bouclier, et inversement.
- **Sac (`I`)** : 24 objets, `E` ou Entrée pour équiper/retirer. Revente à 40 % du prix ; un objet porté ne se revend pas.
- **Potions** : 2 au départ. **Or** plafonné à 999 999.
- **Forgeron (`F`, armes) et armurier (`R`)** : chaque ville a les deux, chaque village un seul, les forteresses aucun. Achat `1`–`9`, revente `V`. Prix modulés par la réputation.
- **Rangs vendus selon la région** : le rang maximal d'une boutique est 3 dans la première tranche, +1 par tranche (5 au plus) ; le rang minimal est le numéro de la tranche (1 à 4), si bien qu'un forgeron lointain ne vend plus d'équipement de débutant. Une ville vend toujours jusqu'au rang 5. Les étalages générés par IA sont bornés par les mêmes rangs.
- **Étalages générés par IA** : à la première visite, le modèle invente une spécialité et 6 à 8 objets ; le serveur valide tout et jette l'étalage s'il reste moins de 4 objets valides. L'étalage est le même pour tous. Sans clé Mistral, un assortiment de repli déterministe est proposé (`gear/fallback.go`).

## PNJ, dialogues et quêtes

- **PNJ** (`game/npc.go`) : marchand, soigneur, forgeron, armurier, chef de village / maire, villageois, villageois à quêtes, soldats, gardes (mobiles, voir plus bas). On leur parle en marchant dessus (abandon après 60 s).
- **Marchand ambulant** (`game/caravan.go`, `world/caravan.go`) : une route entre deux lieux voisins sur deux porte une **caravane** (forgeron ou armurier ambulant, nom propre). Elle fait l'aller-retour sur la route à une case par seconde, avec 30 s d'arrêt à chaque bout ; sa position ne dépend que de l'heure du serveur (rien à sauvegarder) et elle n'est suivie qu'à moins de 40 cases d'un joueur. Envoyée aux clients comme un monstre de type `caravan` (glyphe `E`), on lui parle en marchant sur elle ; elle s'arrête pendant la conversation et attend si la route est barrée. Elle vend de l'équipement **un rang au-dessus** des forges de sa région (assortiment du repli déterministe, sans modèle de langage ni écriture en base) qui **change chaque jour de jeu** (un achat décidé la veille est refusé). Achat et revente seulement : ni vol, ni quête, ni discussion libre. Les routes gardent leur tracé ordonné (`world.RoadRoutesNear`).
- **Services** : potion à 20 or (revente 8 or) ; soins complets gratuits au niveau 1, ensuite 4 or × niveau. Le chef et le maire renseignent sur les lieux dans un rayon de 3 régions, même sans clé Mistral.
- **Dialogues libres** (Mistral, touche `D` / `T` en anglais) :
  - Le modèle ne produit que du texte, sans effet sur l'or, les objets ou les stats.
  - Modération des entrées, budget de jetons par jour, avis de confidentialité avec accord par compte.
  - Sans clé : fonction absente, phrase d'ambiance des villageois.
- **Mémoire et lore** (`lore/`) : fiche de présentation du joueur (8 lieux au plus), rumeurs (portée 3 régions, délai 5 min, fidélité décroissante, oubli après 14 jours), résumé privé par PNJ, faits vérifiés (niveau, boss, morts, quêtes rendues).
- **Quêtes** (`internal/quest/`, `client/quests.go`) :
  - Le modèle propose, le serveur décide : objectifs vérifiables, récompenses plafonnées, textes nettoyés. Accepter/Refuser avec `A`/`R`.
  - Objectifs : tuer 3 à 12 monstres d'une espèce, entrer dans un lieu voisin, vaincre le seigneur de guerre (niveau 10 minimum), **vaincre le maître d'un donjon secret**.
  - **Quête de donjon** (`game/dungeonquest.go`) : le donjon est à 3 régions au plus du donneur, et il faut avoir le niveau du palier (3, 5, 7, 9 ou 11). Elle est accomplie par la mort de la liche *de ce donjon* (un autre donjon ou le seigneur de guerre ne comptent pas), puis rendue au donneur. Récompense : or de 1,25 × la difficulté (150 + 50 × palier), XP, 2 potions, plafonnée comme tout boss. **Accepter la quête révèle l'entrée** du donjon.
  - Deux sources, même validation : le modèle de langage peut proposer `dungeon` avec un nom de la liste `donjons_secrets` du prompt ; **sans modèle**, un villageois à quêtes ajoute au menu « Demander du travail » (`T` / `W` en anglais), qui propose le donjon accessible le plus proche, pas déjà en cours ni rendu (titre et histoire tirés du catalogue, en français et en anglais).
  - Limites : 5 quêtes ouvertes, une par donneur, 30 min entre deux quêtes d'un même donneur, expiration à 7 jours.
  - **Donneur sans travail** : le `!` d'un donneur disparaît (il s'affiche comme un villageois) tant qu'il ne peut rien confier : quête déjà ouverte chez lui, 30 min de pause après une remise, ou 5 quêtes ouvertes. Il revient tout seul à la fin de la pause. Le modèle est alors prévenu qu'il n'a pas de travail ; si sa quête proposée est écartée (invalide ou non modérée), une ligne système l'indique. Limite : sans modèle, le `!` ne tient pas compte de la présence d'un donjon accessible.
  - Livre de quêtes (`Q`), onglets avec `Tab`, abandon avec `X` deux fois ; on rend la quête au donneur.
- **Réputation** par lieu (village ou ville) : Recherché (≤ −10), Suspect (≤ −5), Inconnu, Connu (3), Estimé (8), Honoré (15). Réduction de 20 % puis 40 % sur soins, potions et équipement ; aucune remise sous Inconnu. Bornée à −20 minimum, sauvegardée (valeurs négatives comprises) et changement de palier annoncé dans le journal. Elle monte en rendant des quêtes et **baisse** de 1 en abandonnant une quête, de 3 en se faisant prendre à voler, de 5 en tuant un garde.
- **Vol** : le marchand propose « Faire les poches » (`steal`). Chance de 50 % modulée par le charisme (20 % à 80 %) : succès = quelques pièces d'or (5 + 3 × niveau + 0 à 5), échec = −3 de réputation dans le lieu. Un même marchand ne se laisse pas voler deux fois en 3 minutes (mémoire du serveur seulement).
- **Gardes de villages et de villes** (`game/guards.go`, `world/guards.go`) :
  - Un village a 2 gardes, une ville 5 ; les forteresses aucun. Leur identité (nom unique dans le lieu, personnalité) et leur case de départ sont déterministes, tirées de la seed sur un flux séparé (les plans existants ne bougent pas). Ils ne sont pas dans `Structure.NPCs` : le serveur les fait apparaître quand un joueur est à moins de 40 cases du lieu et les retire quand plus personne n'est près ; un garde vaincu est remplacé après 5 min.
  - **Patrouille** : ils vont de point de passage en point de passage sur les chemins du lieu (plus court trajet, pauses, un pas toutes les 0,5 s), même en zone sûre, et restent à 8 cases au plus de l'emprise du lieu. Ils s'arrêtent quand on leur parle.
  - Ils s'affichent comme `G` bleu (même canal que les monstres dans l'instantané, identifiants hors de la plage des monstres). Marcher dessus leur adresse la parole.
  - **Hors-la-loi** : un joueur **Recherché** (réputation ≤ −10) *dans ce lieu* est poursuivi à 12 cases ou moins, même en zone sûre, puis combattu au contact (un pas toutes les 0,3 s : plus lent qu'un joueur, on peut les distancer). Marcher sur un garde quand on est Recherché le combat au lieu de lui parler. La réputation est propre à chaque lieu : être Recherché dans un village ne concerne pas les gardes des autres lieux. Les autres joueurs ne sont jamais attaqués.
  - **Combat** : fiche de monstre « Garde » (PV 60, mise à l'échelle du niveau de la région comme les monstres), sans XP ni butin. Le tuer coûte 5 de réputation de plus.
  - **Dialogue** : menu « Où trouver : soigneur / marchand / forgeron / armurier / chef ou maire ? » (distance en pas et direction vers l'habitant du lieu, ou, s'il n'y en a pas, le lieu voisin le plus proche qui en a un) et « Quels sont les lieux voisins ? » (les trois plus proches, comme le chef). Ces réponses marchent sans clé Mistral.
  - **Discussion libre** (`D`) : le modèle reçoit en plus la liste des habitants du lieu avec métier, direction et distance (`habitants_du_lieu`) et les lieux voisins ; il n'a pas le droit d'inventer un habitant ou un chemin. Il est prévenu de la réputation du joueur, négative comprise.

## Chat et commandes

| Commande | Effet |
|---|---|
| texte libre | chat local (joueurs proches) |
| `/g`, `/global` | chat global |
| `/me` | émote locale |
| `/w`, `/msg`, `/whisper` pseudo message | message privé |
| `/goto x y` ou `/goto nom` | marche automatique (côté client) |
| `/menhir [n° ou nom]` | liste les pierres dressées activées ou voyage de l'une à l'autre (debout sur une pierre) |
| `/help`, `/?` | aide |

Rafale de 5 messages puis 1 par seconde. `PgHaut` / `PgBas` font défiler l'historique, `Ctrl+↑` / `Ctrl+↓` redimensionnent la discussion. Dans la saisie du chat, `↑` / `↓` rappellent les 100 dernières lignes envoyées (commandes comprises), la saisie en cours est retrouvée en bas.

## Touches clavier

| Contexte | Touches |
|---|---|
| Jeu | déplacement (voir plus haut), `Entrée` chat, `I` sac, `P` fiche, `M` carte, `Q` quêtes |
| Quitter | `Ctrl+C` |
| Connexion | `Tab` / `↑↓` changent de champ, `Ctrl+R` bascule connexion/création |
| Création | `R` relance, `↑↓←→` choix |
| Sac | `↑↓` / `jk`, `E` / Entrée, Échap ou `I` |
| Carte | flèches, `+` `-`, `0`, `W`, `X`, Échap ou `M` |
| Quêtes | `Tab`, `↑↓`, `X`, Échap ou `Q` |
| Combat | `A`/`1`, `I`/`2`, `F`/`3`, flèches, Entrée |
| PNJ | lettres traduites selon la langue (dont `T` / `W` en anglais pour « Demander du travail ») |

## Interface et affichage (`internal/ui/`, `internal/render/`)

- Plein écran, fond sombre peint cellule par cellule, sans dépendre du terminal (option `--transparent` pour garder le fond du terminal), logo.
- Panneau latéral de 34 colonnes (jauges XP/PV/PM, caractéristiques) et zone de discussion redimensionnable.
- Terrain avec variantes de glyphes et couleurs, selon le climat de la contrée (`render/climate.go`; pas de climat sous terre) ; joueurs en combat affichés en rouge.
- Latence mesurée par ping/pong et affichée.
- Options du client : `-addr`, `-name`, `-transparent`, `-lang`.

## Sauvegarde et persistance (`internal/store/`)

- **SQLite** en mode WAL : comptes, personnages, sac, quêtes, réputation, donjons découverts (`discoveries`, migration 16), point de réapparition, pierres dressées activées et cartes au trésor (colonnes de `characters`, migrations 17 et 18), lore, étalages (`npc_wares`).
- Écriture asynchrone ; sauvegarde à la déconnexion, après chaque combat, toutes les 30 s, à l'arrêt propre.
- La seed est fixée à la création de la base ; la changer sur une base existante est refusé. La version du générateur de terrain l'est aussi (voir plus haut) : une base existante ne passe jamais à un autre terrain.
- **Côté client** : carte, repères et exploration dans `<UserConfigDir>/sudo-dragon/map.json` (une entrée par seed, et par version du générateur au-delà de la v1 ; écriture différée de 15 s). Le serveur n'en sait rien.
- Non persistés : le monde (recalculé de la seed), les garnisons et les repaires (réinitialisés au redémarrage), les bonus et cooldowns des points d'intérêt (en mémoire) et l'état des donjons (monstres vaincus, coffres ouverts : en mémoire, perdus au redémarrage du serveur). Seules les entrées découvertes sont sauvegardées.

## Réseau, multijoueur et exploitation (`internal/server/`, `internal/protocol/`)

- **WebSocket**, enveloppes JSON ; serveur autoritatif à 10 Hz, un seul goroutine pour l'état du jeu.
- Monde partagé : joueurs visibles en temps réel, chat, messages privés.
- Instantanés fusionnés : un client lent saute des images au lieu d'être déconnecté.
- **Message `dungeons`** : la liste complète des entrées de donjon découvertes (identifiant, nom, position, palier), envoyée à la connexion et à chaque découverte, avec l'identifiant de la dernière trouvée.
- **Message `zone`** : envoyé à l'entrée et à la sortie d'un donjon (identifiant, nom dans la langue du joueur, seed, palier, coffres ouverts), puis à chaque coffre ouvert dans l'instance. Il précède toujours le premier instantané de la nouvelle carte, dont les coordonnées sont locales au plan du donjon.
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
| `cmd/worldview` | dessine une zone du monde en ASCII (`-sites`, `-territories`, `-gen` pour la version du générateur, `-raw -zoom N` pour une vue d'ensemble du terrain brut avec la part de chaque tuile) |

`Makefile` : build, test, race, check, load, llm-smoke. Go 1.27 requis.

## En cours et limites

- **Gardes** : leur état (position, trajet, mémoire des vols) n'est pas sauvegardé et un redémarrage du serveur les replace à leur poste. Rien ne fait remonter la réputation avec le temps : un joueur Recherché doit rendre des quêtes (pas de grâce, pas de rançon). Les villageois et les marchands ne réagissent ni au vol ni aux combats. Un garde ne sait pas contourner un obstacle autre qu'un joueur passager (il repart vers un autre point de passage après un blocage).

- Pas de compétences ni de sorts à apprendre : la magie est un style d'attaque lié à l'arme.
- Pas de reconnexion automatique après une coupure.
- Un seul processus serveur, sans montée en charge horizontale.
- Le README est en partie périmé (Défendre, carte, i18n) ; sa section « Limites connues » a été revérifiée.
- Le trafic croît comme le carré du nombre de joueurs d'une même zone : chaque instantané contient toutes les entités visibles, sans index spatial ni encodage en différences.
- Les garnisons de forteresse ne sont pas sauvegardées : un redémarrage les remet toutes en place, y compris le seigneur de guerre.
- Un joueur qui se déconnecte en plein combat garde ses PV du moment sans subir la défaite éventuelle.
- **Donjons** :
  - Le client recalcule tout le monde depuis la seed, donc il connaît la position de toutes les entrées : seuls l'affichage et la marche automatique passent par les découvertes, un client modifié pourrait lire les autres.
  - L'état d'un donjon n'est pas sauvegardé en base et n'est jamais libéré tant que son boss vit : un serveur qui tourne longtemps garde en mémoire tous les donjons visités et non terminés.
  - Le donjon est partagé par tous : pas d'instance privée par groupe ni d'invitation, et on peut y retrouver des monstres qui ont suivi un joueur jusqu'à l'escalier.
  - Les monstres de donjon ne repoussent pas.
