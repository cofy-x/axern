package container

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockIoUtil_WriteFile(t *testing.T) {
	type fields struct {
		SuccessMap map[string]bool
	}
	type args struct {
		filename string
		data     []byte
		perm     os.FileMode
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "test",
			fields: fields{
				SuccessMap: map[string]bool{
					"test": true,
				},
			},
			args: args{
				filename: "test",
				data:     []byte("test"),
				perm:     0644,
			},
			wantErr: assert.NoError,
		},
		{
			name: "test for empty mock map",
			fields: fields{
				SuccessMap: nil,
			},
			args: args{
				filename: "test",
				data:     []byte("test"),
				perm:     0644,
			},
			wantErr: assert.Error,
		},
		{
			name: "test",
			fields: fields{
				SuccessMap: map[string]bool{
					"test": true,
				},
			},
			args: args{
				filename: "test",
				data:     []byte("no"),
				perm:     0644,
			},
			wantErr: assert.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MockIoUtil{
				SuccessMap: tt.fields.SuccessMap,
			}
			tt.wantErr(t, m.WriteFile(tt.args.filename, tt.args.data, tt.args.perm), fmt.Sprintf("WriteFile(%v, %v, %v)", tt.args.filename, tt.args.data, tt.args.perm))
		})
	}
}

func TestOs(t *testing.T) {
	m := &MockIoUtil{SuccessMap: map[string]bool{"success": true}}
	f, err := m.Stat("test")
	assert.NoError(t, err)
	assert.Nil(t, f)

	assert.Nil(t, m.MkdirAll("@test", 0755))
	assert.Nil(t, m.WriteFile("@axx", []byte("success"), 0644))
	assert.NotNil(t, m.WriteFile("@axx", []byte("xx"), 0644))
}
