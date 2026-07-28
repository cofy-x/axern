package truncindex

import (
	"fmt"
	"strings"
	"testing"
)

/*
Record(2023-04-14):
BenchmarkIDGenerator_GetId
BenchmarkIDGenerator_GetId/TruncGenerator-threads=1000
BenchmarkIDGenerator_GetId/TruncGenerator-threads=1000-10         	  440326	      2936 ns/op
BenchmarkIDGenerator_GetId/TruncGenerator-threads=10000
BenchmarkIDGenerator_GetId/TruncGenerator-threads=10000-10        	  416594	      3215 ns/op
BenchmarkIDGenerator_GetId/FixLenGenerator-threads=1000
BenchmarkIDGenerator_GetId/FixLenGenerator-threads=1000-10        	  718179	      1972 ns/op
BenchmarkIDGenerator_GetId/FixLenGenerator-threads=10000
BenchmarkIDGenerator_GetId/FixLenGenerator-threads=10000-10       	  531518	      2215 ns/op
*/
func BenchmarkIDGenerator_GetId(b *testing.B) {
	g := NewTruncGenerator("test", []string{})
	f := NewFixLenGenerator(10, []string{})
	testsThreads := []int{1000, 10000}

	for _, threads := range testsThreads {
		b.Run(fmt.Sprintf("TruncGenerator-threads=%d", threads), func(b *testing.B) {
			b.SetParallelism(threads)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					_, err := g.GetID()
					if err != nil {
						b.Errorf("TruncGenerator failed to generate id")
					}
				}
			})
		})
	}

	for _, threads := range testsThreads {
		b.Run(fmt.Sprintf("FixLenGenerator-threads=%d", threads), func(b *testing.B) {
			b.SetParallelism(threads)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					_, err := f.GetID()
					if err != nil {
						b.Errorf("FixLenGenerator failed to generate id")
					}
				}
			})
		})
	}
}

func TestFixLenGenerator_GetId(t *testing.T) {
	type fields struct {
		idIndex  *TruncIndex
		len      int
		modifies []IdModifier
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "simple generator",
			fields: fields{
				idIndex: NewTruncIndex([]string{}),
				len:     10,
			},
		},
		{
			name: "simple generator with modifies",
			fields: fields{
				idIndex: NewTruncIndex([]string{}),
				len:     10,
				modifies: []IdModifier{
					func(id string) string {
						return id + "-suffix"
					},
					func(id string) string {
						return "prefix-" + id
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &FixLenGenerator{
				idIndex:  tt.fields.idIndex,
				len:      tt.fields.len,
				modifies: tt.fields.modifies,
			}
			result := make(map[string]bool)
			got, err := f.GetID()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if _, ok := result[got]; ok {
				t.Errorf("GetID() duplicate id = %v", got)
			}

			if f.modifies != nil {
				sample := ""
				for _, modify := range f.modifies {
					sample = modify(sample)
				}
				if len(sample) != len(got)-f.len {
					t.Errorf("GetID() len = %v, want %v", len(got), len(sample)+f.len)
				}
			}

			result[got] = true
		})
	}
}

func TestFixLenGenerator_ReleaseId(t *testing.T) {
	generator := NewFixLenGenerator(8, nil)
	id, err := generator.GetID()
	if err != nil {
		t.Fatalf("GetID() error = %v", err)
	}
	if generator.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", generator.Len())
	}

	generator.ReleaseId(id)
	if generator.Len() != 0 {
		t.Fatalf("Len() after ReleaseId() = %d, want 0", generator.Len())
	}
}

func TestNewFixLenGenerator(t *testing.T) {
	generator := NewFixLenGenerator(6, []string{"pre-existing"}, PrefixModifier("cg-"))
	if generator.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", generator.Len())
	}

	id, err := generator.GetID()
	if err != nil {
		t.Fatalf("GetID() error = %v", err)
	}
	if !strings.HasPrefix(id, "cg-") {
		t.Fatalf("GetID() = %q, want cg- prefix", id)
	}
}

func TestNewTruncGenerator(t *testing.T) {
	generator := NewTruncGenerator("container", []string{"container-existing"})
	if generator.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", generator.Len())
	}
}

func TestTruncGenerator_GetId(t *testing.T) {
	generator := NewTruncGenerator("container", nil)
	id, err := generator.GetID()
	if err != nil {
		t.Fatalf("GetID() error = %v", err)
	}
	if !strings.HasPrefix(id, "container-") {
		t.Fatalf("GetID() = %q, want container- prefix", id)
	}
	if generator.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", generator.Len())
	}
}

func TestTruncGenerator_Len(t *testing.T) {
	generator := NewTruncGenerator("container", []string{"container-a", "container-b"})
	if generator.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", generator.Len())
	}
}

func TestTruncGenerator_ReleaseId(t *testing.T) {
	generator := NewTruncGenerator("container", nil)
	id, err := generator.GetID()
	if err != nil {
		t.Fatalf("GetID() error = %v", err)
	}
	generator.ReleaseId(id)
	if generator.Len() != 0 {
		t.Fatalf("Len() after ReleaseId() = %d, want 0", generator.Len())
	}
}

func Test_randSeq(t *testing.T) {
	got := randSeq(12)
	if len(got) != 12 {
		t.Fatalf("randSeq() length = %d, want 12", len(got))
	}
	for _, char := range got {
		if !strings.ContainsRune(string(letters), char) {
			t.Fatalf("randSeq() contains unexpected character %q", char)
		}
	}
}
