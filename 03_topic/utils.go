package main

import (
	"iter"
	"math/rand/v2"
	"slices"

	"github.com/google/uuid"
)

// UserIDSampler holds a set of user IDs and their cumulative weights for sampling.
type UserIDSampler struct {
	ids        []uuid.UUID
	cumWeights []float64 // cumulative normalised weights
}

// NewUserIDSampler generates n random UUIDs and pre-computes cumulative
// weights for O(log n) sampling.
func NewUserIDSampler(n int) *UserIDSampler {
	if n <= 0 {
		panic("NewUserIDSampler: n must be > 0")
	}
	ids := make([]uuid.UUID, n)
	cumWeights := make([]float64, n)

	var total float64
	for i := range ids {
		ids[i] = uuid.New()
		w := rand.Float64()
		total += w
		cumWeights[i] = total
	}
	// Normalise so the last entry is exactly 1.0.
	for i := range cumWeights {
		cumWeights[i] /= total
	}
	return &UserIDSampler{ids: ids, cumWeights: cumWeights}
}

// Sample returns a UserID drawn from the weighted distribution.
func (s *UserIDSampler) Sample() uuid.UUID {
	r := rand.Float64()
	// Binary search for the first cumulative weight >= r.
	lo, hi := 0, len(s.cumWeights)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if s.cumWeights[mid] < r {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return s.ids[lo]
}

// IDs returns a copy of the underlying user ID slice (useful for tests).
func (s *UserIDSampler) IDs() []uuid.UUID {
	out := make([]uuid.UUID, len(s.ids))
	copy(out, s.ids)
	return out
}

type Entry struct {
	Id     uuid.UUID
	Weight float64
}

// All returns an iterator over all user IDs in descending weight order (most frequent first).
func (s *UserIDSampler) All() iter.Seq[Entry] {
	// Build index slice sorted by descending individual weight.
	// Individual weight of i = cumWeights[i] - cumWeights[i-1].

	entries := make([]Entry, len(s.ids))
	for i, id := range s.ids {
		w := s.cumWeights[i]
		if i > 0 {
			w -= s.cumWeights[i-1]
		}
		entries[i] = Entry{id, w}
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		// Descending: higher weight comes first.
		if a.Weight > b.Weight {
			return -1
		}
		if a.Weight < b.Weight {
			return 1
		}
		return 0
	})
	return func(yield func(Entry) bool) {
		for _, e := range entries {
			if !yield(e) {
				return
			}
		}
	}
}
