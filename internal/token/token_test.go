package token

import "testing"

func TestGenerate(t *testing.T) {
	tok, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Fatalf("expected 64 chars, got %d", len(tok))
	}
	if !Validate(tok) {
		t.Fatalf("generated token failed validation: %s", tok)
	}
}

func TestGenerateUnique(t *testing.T) {
	a, _ := Generate()
	b, _ := Generate()
	if a == b {
		t.Fatal("two generated tokens should not be equal")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"abc", false},
		{"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", false},
		{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", true},
		{"0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF", true},
	}
	for _, tt := range tests {
		if got := Validate(tt.input); got != tt.want {
			t.Errorf("Validate(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
