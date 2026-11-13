package secretbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeyFileAndSeal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "k", "master.key")
	k1, created, err := LoadOrCreateKey(p)
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", st.Mode())
	}
	k2, created, err := LoadOrCreateKey(p)
	if err != nil || created || string(k1) != string(k2) {
		t.Fatal("reload changed key")
	}
	b, _ := New(k1)
	s := b.Seal([]byte("hunter2"), "totp:1")
	pt, err := b.Open(s, "totp:1")
	if err != nil || string(pt) != "hunter2" {
		t.Fatal("round trip")
	}
	if _, err := b.Open(s, "totp:2"); err == nil {
		t.Fatal("aad not enforced")
	}
}
