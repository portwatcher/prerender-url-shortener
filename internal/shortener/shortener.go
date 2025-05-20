package shortener

import (
	"crypto/rand"
	"math/big"
)

const shortCodeLength = 6 // Length of the generated short code

// customAlphabet excludes characters that can be easily confused (e.g., 0/O, 1/l/I).
const customAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateShortCode creates a random, URL-safe, and more readable short code.
// It does not check for collisions; that should be handled by the caller.
func GenerateShortCode() (string, error) {
	bytes := make([]byte, shortCodeLength)
	alphabetLength := big.NewInt(int64(len(customAlphabet)))

	for i := range bytes {
		num, err := rand.Int(rand.Reader, alphabetLength)
		if err != nil {
			return "", err
		}
		bytes[i] = customAlphabet[num.Int64()]
	}
	return string(bytes), nil
}
