package token

import (
	"testing"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestGenerate(t *testing.T) {
	tok, err := Generate()
	require.Nil(t, err)

	require.Equal(t, 64, len(tok))

	require.True(t, Validate(tok))

}

func TestGenerateUnique(t *testing.T) {
	a, _ := Generate()
	b, _ := Generate()
	require.NotEqual(t, b, a)

}

func TestValidate(t *testing.T) {
	tests := []struct {
		input	string
		want	bool
	}{
		{"", false},
		{"abc", false},
		{"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", false},
		{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", true},
		{"0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF", true},
	}
	for _, tt := range tests {
		got := Validate(tt.input)
		assert.Equal(t, tt.want, got)

	}
}
