package store

// Backend associe le magasin SQL à l'écrivain asynchrone: c'est ce que le
// serveur de jeu utilise. Les lectures voient les sauvegardes encore en file.
type Backend struct {
	Store     *Store
	Persister *Persister
}

// CreateAccount crée un compte.
func (b Backend) CreateAccount(name, password string) (string, error) {
	return b.Store.CreateAccount(name, password)
}

// Authenticate vérifie des identifiants.
func (b Backend) Authenticate(name, password string) (string, error) {
	return b.Store.Authenticate(name, password)
}

// LoadCharacter renvoie l'état le plus récent d'un personnage (nil s'il n'existe pas).
func (b Backend) LoadCharacter(name string) (*Character, error) { return b.Persister.Load(name) }

// SaveCharacter met un personnage en file d'écriture, sans bloquer.
func (b Backend) SaveCharacter(name string, c Character) { b.Persister.Save(name, c) }
