package conf

import (
	"reflect"
	"testing"
)

func TestValkeyEndpoints(t *testing.T) {
	tests := []struct {
		name string
		in   Valkey
		want []string
	}{
		{
			name: "addrs preferred when set (cluster seeds)",
			in:   Valkey{Addr: "ignored:6379", Addrs: []string{"a:6379", "b:6379", "c:6379"}},
			want: []string{"a:6379", "b:6379", "c:6379"},
		},
		{
			name: "single addr when addrs empty",
			in:   Valkey{Addr: "localhost:6379"},
			want: []string{"localhost:6379"},
		},
		{
			name: "addrs whitespace trimmed and blanks dropped",
			in:   Valkey{Addrs: []string{" a:6379 ", "", "b:6379"}},
			want: []string{"a:6379", "b:6379"},
		},
		{
			name: "empty when nothing set",
			in:   Valkey{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.Endpoints()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Endpoints() = %v, want %v", got, tt.want)
			}
		})
	}
}
