package main

import (
	"math"
	"testing"

	"github.com/google/uuid"
)


func TestUserIDSampler(t *testing.T) {
	t.Run("PanicsOnZero", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for n=0")
			}
		}()
		NewUserIDSampler(0)
	})

	t.Run("IDCount", func(t *testing.T) {
		for _, n := range []int{1, 5, 100} {
			s := NewUserIDSampler(n)
			if got := len(s.IDs()); got != n {
				t.Errorf("n=%d: got %d IDs, want %d", n, got, n)
			}
		}
	})

	t.Run("UniqueIDs", func(t *testing.T) {
		s := NewUserIDSampler(50)
		seen := make(map[uuid.UUID]struct{}, 50)
		for _, id := range s.IDs() {
			if _, dup := seen[id]; dup {
				t.Fatalf("duplicate user ID: %s", id)
			}
			seen[id] = struct{}{}
		}
	})

	t.Run("CumulativeWeightsNormalised", func(t *testing.T) {
		for _, n := range []int{1, 3, 10, 50} {
			s := NewUserIDSampler(n)
			last := s.cumWeights[len(s.cumWeights)-1]
			if math.Abs(last-1.0) > 1e-9 {
				t.Errorf("n=%d: last cumulative weight = %v, want 1.0", n, last)
			}
		}
	})
}

func TestSample(t *testing.T) {
	t.Run("ReturnsKnownID", func(t *testing.T) {
		s := NewUserIDSampler(10)
		known := make(map[uuid.UUID]struct{}, 10)
		for _, id := range s.IDs() {
			known[id] = struct{}{}
		}
		for range 1000 {
			got := s.Sample()
			if _, ok := known[got]; !ok {
				t.Fatalf("Sample() returned unknown ID %q", got)
			}
		}
	})

	// FrequenciesMatchWeights verifies that observed sampling frequencies are
	// close to the precomputed weights by iterating over All() in weight order.
	// Tolerance is 3σ for a binomial with p=weight, n=trials.
	//t.Run("FrequenciesMatchWeights", func(t *testing.T) {
	//	const trials = 200_000
	//	for _, n := range []int{5, 20} {
	//		s := NewUserIDSampler(n)
	//		counts := make(map[uuid.UUID]int, n)
	//		for range trials {
	//			counts[s.Sample()]++
	//		}
	//		// Iterate in All() order; each Entry carries the precomputed weight.
	//		for e := range s.All() {
	//			observed := float64(counts[e.Id]) / trials
	//			sigma := math.Sqrt(e.Weight * (1 - e.Weight) / trials)
	//			if math.Abs(observed-e.Weight) > 3*sigma {
	//				t.Errorf("n=%d id=%s: observed %.5f, expected %.5f (±3σ=%.5f)",
	//					n, e.Id, observed, e.Weight, 3*sigma)
	//			}
	//		}
	//	}
	//})
}
