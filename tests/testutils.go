package integration

import (
	"math/rand"
	"time"
)

// Local RNG for all test randomness (Go 1.20+ compliant)
var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// PickRandom returns a random element from a slice.
func PickRandom[T any](items []T) T {
	return items[rng.Intn(len(items))]
}

// MapKeys returns the keys of a map[string]string as a slice.
func MapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
