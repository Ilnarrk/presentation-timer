//go:build windows

package conference

import (
	"testing"
	"unsafe"
)

func TestICoreWebView2_8VtblPutIsMutedSlot(t *testing.T) {
	var v iCoreWebView2_8Vtbl
	slot := unsafe.Offsetof(v.putIsMuted) / unsafe.Sizeof(v.putIsMuted)
	if slot != 84 {
		t.Fatalf("putIsMuted vtable slot = %d, want 84 (IUnknown+ICoreWebView2…_7+3)", slot)
	}
}
