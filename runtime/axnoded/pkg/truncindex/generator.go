package truncindex

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
)

type UniqueIdGenerator interface {
	GetID() (string, error)
	ReleaseId(id string)

	Len() int
}

func init() {
	rand.New(rand.NewSource(time.Now().UnixNano()))
}

// TruncGenerator is a generator for standard uuid string with prefix.
type TruncGenerator struct {
	idIndex *TruncIndex
	prefix  string
}

func NewTruncGenerator(prefix string, ids []string) *TruncGenerator {
	return &TruncGenerator{
		idIndex: NewTruncIndex(ids),
		prefix:  prefix,
	}
}

func (g *TruncGenerator) GetID() (string, error) {
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("%s-%s", g.prefix, uuid.New().String())
		if err := g.idIndex.Add(id); err != nil {
			continue
		}
		return id, nil
	}
	return "", fmt.Errorf("failed to generate id")
}

func (g *TruncGenerator) Len() int {
	return g.idIndex.Len()
}

func (g *TruncGenerator) ReleaseId(id string) {
	_ = g.idIndex.Delete(id)
}

func NewFixLenGenerator(len int, ids []string, modifies ...IdModifier) *FixLenGenerator {
	return &FixLenGenerator{
		idIndex:  NewTruncIndex(ids),
		len:      len,
		modifies: modifies,
	}
}

type IdModifier func(id string) string

func PrefixModifier(prefix string) IdModifier {
	return func(id string) string {
		return prefix + id
	}
}

func SuffixModifier(suffix string) IdModifier {
	return func(id string) string {
		return id + suffix
	}
}

// FixLenGenerator is a generator that generates fixed length id, used for short id request. Additionally, it supports prefix.
type FixLenGenerator struct {
	idIndex  *TruncIndex
	len      int
	modifies []IdModifier
}

func (f *FixLenGenerator) GetID() (string, error) {
	for i := 0; i < 10000; i++ {
		rawId := randSeq(f.len)
		for _, modify := range f.modifies {
			rawId = modify(rawId)
		}

		if err := f.idIndex.Add(rawId); err != nil {
			continue
		}
		return rawId, nil
	}
	return "", fmt.Errorf("failed to generate id")
}

func (f *FixLenGenerator) ReleaseId(id string) {
	_ = f.idIndex.Delete(id)
}

func (f *FixLenGenerator) Len() int {
	return f.idIndex.Len()
}

var _ UniqueIdGenerator = &FixLenGenerator{}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890")

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
