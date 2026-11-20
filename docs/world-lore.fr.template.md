<!--
  MODÈLE DU LORE DU MONDE (français)

  Le lore est la connaissance partagée par TOUS les PNJ : ce que chaque habitant de ce monde
  sait, du forgeron au garde. Il est ajouté à chaque dialogue et à la génération du stock des
  forges.

  MODE D'EMPLOI
  1. Copiez ce fichier à côté du serveur sous le nom `world-lore.fr.md` (ou dans le dossier
     donné par `-world-lore DIR`). Les fichiers `world-lore.*.md` sont ignorés par git.
  2. Écrivez sous chaque titre. Tout commentaire HTML (comme ce bloc) est retiré
     avant l'envoi au modèle, et un titre sans texte dessous est retiré aussi : vous pouvez donc
     laisser vides les sections qui ne vous servent pas.
  3. Faites la version anglaise `world-lore.en.md` (modèle : `docs/world-lore.en.template.md`)
     et GARDEZ LES DEUX ALIGNÉES. Si un seul fichier existe, il sert à toutes les langues et le
     modèle le traduit à la volée.
  4. `kill -HUP <pid du serveur>` relit les fichiers sans redémarrer ; un fichier invalide
     (UTF-8 cassé, commentaire jamais fermé) garde l'ancienne version. Les stocks de forge déjà
     générés ne changent pas : `server -wares-reset RX,RY,RANK` les régénère.

  CONSEILS D'ÉCRITURE
  - Taille maximale : 8000 caractères par langue (le reste est coupé). Chaque caractère coûte des
    jetons à chaque réplique : allez à l'essentiel, en phrases courtes et factuelles.
  - Écrivez des FAITS à la troisième personne, pas des ordres au PNJ. Le ton, les manières de
    parler et le caractère de chaque PNJ viennent déjà de sa fiche personnelle.
  - Tout PNJ peut répéter ce qui est écrit ici à un joueur : pas de secret, pas de spoiler.
  - Peuples du jeu : Humains, Elfes, Nains, Halfelins. Les rancunes entre peuples sont déjà
    réglées par le jeu (pourcentage de haine) : décrivez leur culture et leur histoire, pas leurs
    insultes.
  - N'écrivez ni nom de lieu réel du jeu (le serveur les fournit aux chefs, maires et gardes) ni
    chiffre de jeu (or, dégâts, niveaux) : le jeu en décide. Les régions et royaumes lointains
    ou légendaires de ce lore sont, eux, bienvenus.
  - Aucune référence au monde réel (pays, religions, personnes, marques).
-->

# Lore du monde

## 1. Le monde en quelques phrases
<!-- Le résumé que tout le monde pourrait donner à un étranger. 3 à 5 phrases : nom du monde,
     ce qui le distingue, l'ambiance générale. Ex. « Les Terres Brisées portent encore les
     cicatrices de la Chute des Étoiles… » -->

## 2. Ton et limites
<!-- Registre voulu (sombre, populaire, merveilleux, humoristique…), niveau de technologie
     (médiéval, pas de poudre à canon ?), sujets que les PNJ évitent ou ne connaissent pas. -->

## 3. Origines et cosmologie
<!-- Comment le monde est né, selon la croyance commune. Le soleil, la lune, les saisons,
     les astres ; les âges du monde. -->

## 4. Histoire : les grandes dates
<!-- 4 à 8 événements que tous connaissent, avec une datation relative (« il y a trois
     siècles »). Guerres, chutes d'empires, fondations, cataclysmes, dernière grande paix. -->

## 5. Les peuples
<!-- Un paragraphe par peuple du jeu : Humains, Elfes, Nains, Halfelins.
     Pour chacun : origine, vie et coutumes, métiers de prédilection, valeurs, ce que les
     autres en pensent, vieux différends (sans insultes). Ajoutez les peuples légendaires ou
     disparus si besoin. -->

### Humains

### Elfes

### Nains

### Halfelins

## 6. Magie et surnaturel
<!-- La magie existe-t-elle, qui la pratique, est-elle crainte, rare, interdite ? Ses
     contreparties. Liches, esprits, malédictions : ce que le commun en sait. -->

## 7. Croyances et religions
<!-- Divinités ou forces vénérées, cultes, ordres religieux, rites (naissance, mort), tabous,
     serments. Les guérisseurs relèvent-ils d'un culte ? -->

## 8. Pouvoir et société
<!-- Qui gouverne (rois, conseils, seigneurs), comment vivent villages, villes et forteresses,
     justice et gardes, guildes, impôts, classes sociales, esclavage ou liberté. -->

## 9. Économie et métiers
<!-- La monnaie (l'or du jeu : comment l'appelle-t-on ?), ce qui est précieux ou rare, les
     matériaux réputés (métaux, bois, cuirs), les routes et les caravanes, la place des forgerons
     et armuriers. Sert aussi à nommer et décrire les objets des forges. -->

## 10. Dangers et créatures
<!-- Les monstres vus comme le voient les habitants : d'où viennent-ils, que craint-on, les
     seigneurs de guerre, les donjons secrets et leurs maîtres, les légendes de bêtes. Reste
     qualitatif : le jeu décide des vraies créatures et de leurs forces. -->

## 11. Géographie générale
<!-- Grandes régions, mers, montagnes, climats, royaumes lointains, terres inconnues. Aucune
     position précise de lieu du jeu : les gardes, chefs et maires reçoivent la vraie
     liste des lieux voisins. -->

## 12. Coutumes et vie quotidienne
<!-- Salutations, politesse, fêtes et saisons, cuisine et boissons, jeux, proverbes,
     jurons anodins, superstitions, façons de compter le temps. -->

## 13. Légendes et mystères communs
<!-- Histoires que tout le monde raconte : héros, artefacts, trésors perdus, prophéties,
     mystères non résolus. Du vrai et du douteux : les PNJ peuvent en douter. Pas de
     secrets que les joueurs devraient découvrir par eux-mêmes. -->
