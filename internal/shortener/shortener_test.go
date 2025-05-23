package shortener

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateShortCode(t *testing.T) {
	tests := []struct {
		name         string
		iterations   int
		expectUnique bool
	}{
		{
			name:         "single generation",
			iterations:   1,
			expectUnique: true,
		},
		{
			name:         "multiple generations should be unique",
			iterations:   100,
			expectUnique: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generated := make(map[string]bool)

			for i := 0; i < tt.iterations; i++ {
				code, err := GenerateShortCode()
				assert.NoError(t, err)
				assert.NotEmpty(t, code)
				assert.Len(t, code, shortCodeLength)

				// Check that all characters are from the custom alphabet
				for _, char := range code {
					assert.Contains(t, customAlphabet, string(char))
				}

				if tt.expectUnique {
					// Check for duplicates
					assert.False(t, generated[code], "Generated duplicate short code: %s", code)
					generated[code] = true
				}
			}
		})
	}
}

func TestShortCodeProperties(t *testing.T) {
	code, err := GenerateShortCode()
	assert.NoError(t, err)

	// Test length
	assert.Len(t, code, shortCodeLength)

	// Test that it only contains allowed characters
	for _, char := range code {
		assert.Contains(t, customAlphabet, string(char))
	}

	// Test that it doesn't contain confusing characters
	confusingChars := []string{"0", "O", "1", "l", "I"}
	for _, confusing := range confusingChars {
		assert.NotContains(t, code, confusing)
	}
}

func TestCustomAlphabet(t *testing.T) {
	// Test that custom alphabet doesn't contain confusing characters
	confusingChars := []string{"0", "O", "1", "l", "I"}
	for _, confusing := range confusingChars {
		assert.NotContains(t, customAlphabet, confusing)
	}

	// Test alphabet length (should be reasonable for collision avoidance)
	assert.Greater(t, len(customAlphabet), 20, "Alphabet should be long enough to avoid collisions")

	// Test that alphabet contains both letters and numbers
	hasLetter := false
	hasNumber := false
	for _, char := range customAlphabet {
		if char >= 'A' && char <= 'Z' {
			hasLetter = true
		}
		if char >= '2' && char <= '9' {
			hasNumber = true
		}
	}
	assert.True(t, hasLetter, "Alphabet should contain letters")
	assert.True(t, hasNumber, "Alphabet should contain numbers")
}

func TestShortCodeLength(t *testing.T) {
	assert.Equal(t, 6, shortCodeLength, "Short code length should be 6")
}

func TestGenerateShortCodeUniqueness(t *testing.T) {
	// Generate a large number of codes to test for reasonable uniqueness
	const numCodes = 1000
	codes := make(map[string]bool)

	for i := 0; i < numCodes; i++ {
		code, err := GenerateShortCode()
		assert.NoError(t, err)

		// Check for duplicates
		if codes[code] {
			t.Errorf("Generated duplicate short code: %s after %d iterations", code, i+1)
			break
		}
		codes[code] = true
	}

	assert.Len(t, codes, numCodes, "All generated codes should be unique")
}

func TestGenerateShortCodeFormat(t *testing.T) {
	code, err := GenerateShortCode()
	assert.NoError(t, err)

	// Check format: should be all uppercase letters and numbers
	assert.True(t, strings.ToUpper(code) == code, "Short code should be uppercase")

	// Check that it's URL safe (no special characters)
	urlUnsafeChars := []string{"/", "\\", "?", "#", "[", "]", "@", "!", "$", "&", "'", "(", ")", "*", "+", ",", ";", "=", "%", " "}
	for _, unsafe := range urlUnsafeChars {
		assert.NotContains(t, code, unsafe, "Short code should not contain URL-unsafe character: %s", unsafe)
	}
}

func BenchmarkGenerateShortCode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := GenerateShortCode()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestGenerateShortCodeConcurrency(t *testing.T) {
	const numGoroutines = 10
	const codesPerGoroutine = 100

	resultChan := make(chan string, numGoroutines*codesPerGoroutine)
	doneChan := make(chan bool, numGoroutines)

	// Start multiple goroutines generating codes
	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < codesPerGoroutine; j++ {
				code, err := GenerateShortCode()
				if err != nil {
					t.Errorf("Error generating short code: %v", err)
					return
				}
				resultChan <- code
			}
			doneChan <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-doneChan
	}
	close(resultChan)

	// Collect all generated codes
	codes := make(map[string]bool)
	for code := range resultChan {
		assert.False(t, codes[code], "Generated duplicate short code in concurrent test: %s", code)
		codes[code] = true
	}

	expectedTotal := numGoroutines * codesPerGoroutine
	assert.Len(t, codes, expectedTotal, "Expected %d unique codes, got %d", expectedTotal, len(codes))
}
