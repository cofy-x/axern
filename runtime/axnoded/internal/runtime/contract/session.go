package contract

type Chunk struct {
	Stdout []byte
	Stderr []byte
}

type Session interface {
	Write([]byte) error
	CloseStdin() error
	Resize(cols, rows uint32) error
	Signal(signal string) error
	Recv() (Chunk, error)
	Wait() (Exit, error)
	Close() error
}
