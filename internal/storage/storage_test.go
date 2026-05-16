package storage

import (
	"os"
	"testing"
	"time"

	"github.com/wow-look-at-my/log-streamer/internal/protocol"
)

func TestAppendAndFetch(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	msg := protocol.StreamMessage{
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Line:      "hello",
		Stream:    "stdout",
	}

	if err := store.Append(tok, msg); err != nil {
		t.Fatal(err)
	}

	lines, err := store.Fetch(tok)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0].Line != "hello" {
		t.Fatalf("expected 'hello', got %q", lines[0].Line)
	}
	if lines[0].Stream != "stdout" {
		t.Fatalf("expected 'stdout', got %q", lines[0].Stream)
	}
}

func TestMultipleAppends(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for i := 0; i < 5; i++ {
		msg := protocol.StreamMessage{
			Timestamp: time.Now().UTC(),
			Line:      "line",
			Stream:    "stdout",
		}
		if err := store.Append(tok, msg); err != nil {
			t.Fatal(err)
		}
	}

	lines, err := store.Fetch(tok)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	msg := protocol.StreamMessage{Timestamp: time.Now().UTC(), Line: "x", Stream: "stdin"}
	store.Append(tok, msg)

	if !store.Exists(tok) {
		t.Fatal("expected token to exist")
	}

	if err := store.Delete(tok); err != nil {
		t.Fatal(err)
	}

	if store.Exists(tok) {
		t.Fatal("expected token to not exist after delete")
	}
}

func TestFetchNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Fetch("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if !os.IsNotExist(err) {
		t.Fatalf("expected not-exist error, got %v", err)
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	tok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if store.Exists(tok) {
		t.Fatal("should not exist before append")
	}

	store.Append(tok, protocol.StreamMessage{Timestamp: time.Now().UTC(), Line: "x", Stream: "stdout"})
	if !store.Exists(tok) {
		t.Fatal("should exist after append")
	}
}
