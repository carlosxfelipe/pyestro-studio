package store

import (
	"path/filepath"
	"testing"
)

func TestStoreSetGetPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	st, err := New("pyestro-studio-test", Options{
		Path:          path,
		EncryptionKey: "test-secret-key",
	})
	if err != nil {
		t.Fatalf("store.New failed: %v", err)
	}

	type sample struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}

	if err := st.Set("cfg", sample{Name: "studio", Port: 8081}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	st2, err := New("pyestro-studio-test", Options{
		Path:          path,
		EncryptionKey: "test-secret-key",
	})
	if err != nil {
		t.Fatalf("reload store.New failed: %v", err)
	}

	var got sample
	ok, err := st2.Get("cfg", &got)
	if err != nil || !ok {
		t.Fatalf("Get failed (ok=%v): %v", ok, err)
	}
	if got.Name != "studio" || got.Port != 8081 {
		t.Fatalf("unexpected decoded value: %+v", got)
	}
}
