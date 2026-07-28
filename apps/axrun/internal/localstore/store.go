package localstore

type Store struct {
	root string
}

func New(root string) Store {
	return Store{root: root}
}
