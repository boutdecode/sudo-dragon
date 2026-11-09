package store

import (
	"log/slog"
	"sync"
	"time"
)

// Persister écrit les personnages en arrière-plan, par lots, pour que la
// boucle de jeu n'attende jamais le disque. Load consulte d'abord les
// sauvegardes pas encore écrites: un joueur qui se reconnecte aussitôt après
// une déconnexion retrouve donc son état le plus récent.
type Persister struct {
	st  *Store
	log *slog.Logger

	mu       sync.Mutex
	pending  map[string]Character // en attente d'écriture
	inflight map[string]Character // en cours d'écriture (visible jusqu'au commit)
	closed   bool

	stop chan struct{}
	done chan struct{}
}

// NewPersister démarre l'écrivain; il vide sa file toutes les interval.
func NewPersister(st *Store, interval time.Duration, log *slog.Logger) *Persister {
	if log == nil {
		log = slog.Default()
	}
	p := &Persister{
		st:      st,
		log:     log,
		pending: make(map[string]Character),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go p.run(interval)
	return p
}

func (p *Persister) run(interval time.Duration) {
	defer close(p.done)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			p.flush()
		case <-p.stop:
			p.flush()
			return
		}
	}
}

// flush écrit la file. En cas d'échec, les entrées retournent en file, sauf
// si une version plus récente est arrivée entre-temps.
func (p *Persister) flush() error {
	p.mu.Lock()
	if len(p.pending) == 0 {
		p.mu.Unlock()
		return nil
	}
	batch := p.pending
	p.pending = make(map[string]Character)
	p.inflight = batch
	p.mu.Unlock()

	err := p.st.SaveCharacters(batch)

	p.mu.Lock()
	defer p.mu.Unlock()
	p.inflight = nil
	if err != nil {
		p.log.Error("sauvegarde des personnages", "err", err, "n", len(batch))
		for account, c := range batch {
			if newer, ok := p.pending[account]; !ok {
				p.pending[account] = c
			} else if (c.QuestsDirty && !newer.QuestsDirty) || (c.GearDirty && !newer.GearDirty) || (c.DiscoveriesDirty && !newer.DiscoveriesDirty) {
				// L'écriture a échoué : la version plus récente doit réécrire ce qui n'a pas été écrit.
				newer.QuestsDirty = newer.QuestsDirty || c.QuestsDirty
				newer.GearDirty = newer.GearDirty || c.GearDirty
				newer.DiscoveriesDirty = newer.DiscoveriesDirty || c.DiscoveriesDirty
				p.pending[account] = newer
			}
		}
	}
	return err
}

// Save met un personnage en file d'écriture (sans bloquer). Sans effet après Close.
func (p *Persister) Save(account string, c Character) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		p.log.Warn("sauvegarde ignorée: écrivain fermé", "account", account)
		return
	}
	if old, ok := p.pending[account]; ok {
		c.QuestsDirty = c.QuestsDirty || old.QuestsDirty // la version précédente n'a pas encore été écrite
		c.GearDirty = c.GearDirty || old.GearDirty
		c.DiscoveriesDirty = c.DiscoveriesDirty || old.DiscoveriesDirty
	}
	p.pending[account] = c
}

// Load renvoie l'état le plus récent connu: file d'attente, écriture en
// cours, puis base. Renvoie nil si le compte n'a pas encore de personnage.
func (p *Persister) Load(account string) (*Character, error) {
	p.mu.Lock()
	if c, ok := p.pending[account]; ok {
		p.mu.Unlock()
		return &c, nil
	}
	if c, ok := p.inflight[account]; ok {
		p.mu.Unlock()
		return &c, nil
	}
	p.mu.Unlock()
	return p.st.LoadCharacter(account)
}

// Close écrit ce qui reste puis arrête l'écrivain. Renvoie l'erreur de la
// dernière écriture si des données n'ont pas pu être enregistrées.
func (p *Persister) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	close(p.stop)
	<-p.done

	p.mu.Lock()
	left := len(p.pending)
	p.mu.Unlock()
	if left > 0 {
		return p.flush() // dernière tentative, pour renvoyer l'erreur
	}
	return nil
}
