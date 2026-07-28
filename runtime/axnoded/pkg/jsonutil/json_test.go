package jsonutil

import (
	"reflect"
	"testing"
)

func TestUnescapedMarshal(t *testing.T) {
	type args struct {
		in interface{}
	}

	type Tmp struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			name: "html escape >",
			args: args{
				in: Tmp{Name: "xxx > yyy"},
			},
			want:    []byte(`{"name":"xxx > yyy"}`),
			wantErr: false,
		},
		{
			name: "complex content",
			args: args{
				in: Tmp{Name: "xxx > yyy\nzzz\n@#$%^&*()_+{}|:\"<>?"},
			},
			want:    []byte(`{"name":"xxx > yyy\nzzz\n@#$%^&*()_+{}|:\"<>?"}`),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnescapedMarshal(tt.args.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnescapedMarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UnescapedMarshal() got = %v, want %v", got, tt.want)
			}
		})
	}
}
