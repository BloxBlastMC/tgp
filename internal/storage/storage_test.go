package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLocal(t *testing.T) {
	ctx := context.Background()
	s, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "logs/1/0001", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(ctx, "logs/1/0001")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "hello" {
		t.Fatalf("%q", b)
	}
	if _, err := s.Get(ctx, "logs/2/x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	for _, bad := range []string{"", "../x", "a/../../b"} {
		if err := s.Put(ctx, bad, strings.NewReader(""), 0); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := s.DeletePrefix(ctx, "logs/1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "logs/1/0001"); !errors.Is(err, ErrNotFound) {
		t.Fatal("not deleted")
	}
}
