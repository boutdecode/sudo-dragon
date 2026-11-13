package store

// Backend pairs the SQL store with the asynchronous writer: this is what the
// game server uses. Reads see saves that are still queued.
type Backend struct {
	Store     *Store
	Persister *Persister
}

// CreateAccount creates an account.
func (b Backend) CreateAccount(name, password string) (string, error) {
	return b.Store.CreateAccount(name, password)
}

// Authenticate checks credentials.
func (b Backend) Authenticate(name, password string) (string, error) {
	return b.Store.Authenticate(name, password)
}

// LoadCharacter returns the most recent state of a character (nil if it does not exist).
func (b Backend) LoadCharacter(name string) (*Character, error) { return b.Persister.Load(name) }

// SaveCharacter queues a character for writing, without blocking.
func (b Backend) SaveCharacter(name string, c Character) { b.Persister.Save(name, c) }
