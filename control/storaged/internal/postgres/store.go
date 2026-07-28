package postgres

type Store struct {
	db *DB
}

func NewStore(db *DB) *Store {
	return &Store{db: db}
}
