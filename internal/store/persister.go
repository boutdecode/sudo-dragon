package store

import (
	"log/slog"
	"sync"
	"time"
)

// Persister writes characters in the background, in batches, so that the
// game loop never waits for the disk. Load first looks at saves not yet
// written: a player who reconnects right after a disconnection therefore
// finds their most recent state.
type Persister struct {
	st  *Store
	log *slog.Logger

	mu       sync.Mutex
	pending  map[string]Character // waiting to be written
	inflight map[string]Character // being written (visible until commit)
	closed   bool

	stop chan struct{}
	done chan struct{}
}

// NewPersister starts the writer; it drains its queue every interval.
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

// flush writes the queue. On failure, entries go back in the queue, unless
// a more recent version arrived in the meantime.
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
		p.log.Error("saving characters", "err", err, "n", len(batch))
		for account, c := range batch {
			if newer, ok := p.pending[account]; !ok {
				p.pending[account] = c
			} else if (c.QuestsDirty && !newer.QuestsDirty) || (c.GearDirty && !newer.GearDirty) || (c.DiscoveriesDirty && !newer.DiscoveriesDirty) {
				// The write failed: the more recent version must rewrite what was not written.
				newer.QuestsDirty = newer.QuestsDirty || c.QuestsDirty
				newer.GearDirty = newer.GearDirty || c.GearDirty
				newer.DiscoveriesDirty = newer.DiscoveriesDirty || c.DiscoveriesDirty
				p.pending[account] = newer
			}
		}
	}
	return err
}

// Save queues a character for writing (without blocking). No effect after Close.
func (p *Persister) Save(account string, c Character) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		p.log.Warn("save ignored: writer closed", "account", account)
		return
	}
	if old, ok := p.pending[account]; ok {
		c.QuestsDirty = c.QuestsDirty || old.QuestsDirty // the previous version has not been written yet
		c.GearDirty = c.GearDirty || old.GearDirty
		c.DiscoveriesDirty = c.DiscoveriesDirty || old.DiscoveriesDirty
	}
	p.pending[account] = c
}

// Load returns the most recent known state: queue, write in
// progress, then the database. Returns nil if the account has no character yet.
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

// Close writes what remains then stops the writer. Returns the error of the
// last write if some data could not be saved.
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
		return p.flush() // last attempt, to return the error
	}
	return nil
}
