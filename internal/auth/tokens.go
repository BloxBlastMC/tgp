package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"strings"
)

// Token prefixes make credentials recognisable (and greppable by secret
// scanners) and tell the server which kind it is.
const (
	PATPrefix    = "tgp_pat_"
	RunnerPrefix = "tgp_run_"
)

// RandomToken returns n random bytes as unpadded base64url.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewPAT returns a new personal access token.
func NewPAT() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return PATPrefix + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

// HashToken returns the hex SHA-256 of a high-entropy token. Tokens are
// random, so a fast hash is sufficient (unlike passwords).
func HashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// tempAlphabet avoids look-alike characters (0/O, 1/l/I).
const tempAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewTempPassword returns a readable random password like "kq7v-Ma3x-9TzR-bw4e".
func NewTempPassword() string {
	return groupedRandom(tempAlphabet, 4, 4)
}

// recoveryAlphabet is lowercase-only so codes are easy to type.
const recoveryAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// NewRecoveryCode returns a code like "x7kq-m2vd-p9tr".
func NewRecoveryCode() string {
	return groupedRandom(recoveryAlphabet, 3, 4)
}

// NormalizeRecoveryCode lowercases a code and strips spaces and dashes.
func NormalizeRecoveryCode(c string) string {
	c = strings.ToLower(c)
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, c)
}

func groupedRandom(alphabet string, groups, size int) string {
	var sb strings.Builder
	max := big.NewInt(int64(len(alphabet)))
	for g := 0; g < groups; g++ {
		if g > 0 {
			sb.WriteByte('-')
		}
		for i := 0; i < size; i++ {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				panic(err)
			}
			sb.WriteByte(alphabet[n.Int64()])
		}
	}
	return sb.String()
}
