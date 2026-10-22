package sim

import (
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/Rakshit-gen/hindsight/logs"
	"github.com/Rakshit-gen/hindsight/policy"
)

// Logger is a live router: it gives its distribution for each attempt and
// hears outcomes when they arrive.
type Logger interface {
	Probs(s policy.State, at int64) map[string]float64
	Learn(gateway string, s policy.State, paid bool, at int64)
}

// Static routes by a fixed policy.
type Static struct{ policy.Policy }

func (x Static) Probs(s policy.State, _ int64) map[string]float64 { return x.Policy.Probs(s) }
func (Static) Learn(string, policy.State, bool, int64)            {}

func candidates(s policy.State) []string {
	var out []string
	for _, g := range s.Eligible {
		if !slices.Contains(s.Tried, g) {
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		return s.Eligible
	}
	return out
}

// Window sends each attempt to the untried gateway with the best success
// rate over its last Size outcomes for the method, and a share Epsilon at
// random. It's the router public write-ups describe.
type Window struct {
	Size    int
	Epsilon float64
	hist    map[string][]bool
}

func (x *Window) Probs(s policy.State, _ int64) map[string]float64 {
	cands := candidates(s)
	best, bestScore := "", -1.0
	for _, g := range cands {
		h := x.hist[g+"|"+s.Context["method"]]
		n := 0
		for _, ok := range h {
			if ok {
				n++
			}
		}
		if sc := (float64(n) + 1) / (float64(len(h)) + 2); sc > bestScore {
			best, bestScore = g, sc
		}
	}
	probs := map[string]float64{}
	for _, g := range cands {
		probs[g] = x.Epsilon / float64(len(cands))
	}
	probs[best] += 1 - x.Epsilon
	return probs
}

func (x *Window) Learn(g string, s policy.State, paid bool, _ int64) {
	if x.hist == nil {
		x.hist = map[string][]bool{}
	}
	key := g + "|" + s.Context["method"]
	h := append(x.hist[key], paid)
	if len(h) > x.Size {
		h = h[1:]
	}
	x.hist[key] = h
}
