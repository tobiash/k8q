package engine

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestEnvMapFromBytes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name:  "basic key=value pairs",
			input: "DB_HOST=localhost\nDB_PORT=5432\n",
			want: map[string]string{
				"DB_HOST": "localhost",
				"DB_PORT": "5432",
			},
		},
		{
			name:  "empty input",
			input: "",
			want:  map[string]string{},
		},
		{
			name:  "skips comments",
			input: "# this is a comment\nKEY=val\n",
			want: map[string]string{
				"KEY": "val",
			},
		},
		{
			name:  "skips empty lines",
			input: "\n\nKEY=val\n\n",
			want: map[string]string{
				"KEY": "val",
			},
		},
		{
			name:  "skips lines without equals",
			input: "NOEQUALS\nKEY=val\n",
			want: map[string]string{
				"KEY": "val",
			},
		},
		{
			name:  "value with equals sign",
			input: "CONN=host=db port=5432\n",
			want: map[string]string{
				"CONN": "host=db port=5432",
			},
		},
		{
			name:  "trims whitespace",
			input: "  KEY  =  value  \n",
			want: map[string]string{
				"KEY": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnvMapFromBytes([]byte(tt.input))
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
