//go:build windows

package conference

import "testing"

func TestCoInitializeResultError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hr      uintptr
		wantErr bool
	}{
		{name: "ok", hr: 0},
		{name: "already initialized", hr: 1},
		{name: "changed mode", hr: rpcEChangedMode},
		{name: "unexpected failure", hr: 0x80004005, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := coInitializeResultError(test.hr)
			if (err != nil) != test.wantErr {
				t.Fatalf("coInitializeResultError(%#x) = %v, wantErr=%v", test.hr, err, test.wantErr)
			}
		})
	}
}
