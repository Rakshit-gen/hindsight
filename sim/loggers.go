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

// Thompson keeps a fading Beta belief per gateway and method. Each minute
// it works out, by sampling, the chance each gateway would win, and then
// routes by those chances. Routing by chances it has written down, rather
// than by one fresh sample, is what makes its logged propensity exact.
type Thompson struct {
	Floor    float64 // share sent uniformly at random, so nothing goes unseen
	HalfLife time.Duration
	Draws    int
	Seed     uint64

	rng    *rand.Rand
	counts map[string]*belief
	cache  map[string]cached
}

type belief struct {
	paid, failed float64
	at           int64
}

type cached struct {
	at    int64
	probs map[string]float64
}

func (x *Thompson) belief(key string, at int64) *belief {
	if x.counts == nil {
		x.counts = map[string]*belief{}
		x.cache = map[string]cached{}
		x.rng = rand.New(rand.NewPCG(x.Seed, 7))
	}
	b := x.counts[key]
	if b == nil {
		b = &belief{at: at}
		x.counts[key] = b
	}
	f := math.Exp2(-float64(at-b.at) / float64(x.HalfLife.Milliseconds()))
	b.paid, b.failed, b.at = b.paid*f, b.failed*f, at
	return b
}

func (x *Thompson) Probs(s policy.State, at int64) map[string]float64 {
	cands := candidates(s)
	key := s.Context["method"] + "|" + strings.Join(cands, ",")
	if c, ok := x.cache[key]; ok && at-c.at < time.Minute.Milliseconds() {
		return c.probs
	}
	bs := make([]*belief, len(cands))
	for i, g := range cands {
		bs[i] = x.belief(g+"|"+s.Context["method"], at)
	}
	wins := make([]float64, len(cands))
	for range x.Draws {
		best, bestV := 0, -1.0
		for i, b := range bs {
			if v := betaSample(x.rng, 1+b.paid, 1+b.failed); v > bestV {
				best, bestV = i, v
			}
		}
		wins[best]++
	}
	probs := map[string]float64{}
	for i, g := range cands {
		probs[g] = (1-x.Floor)*wins[i]/float64(x.Draws) + x.Floor/float64(len(cands))
	}
	x.cache[key] = cached{at, probs}
	return probs
}

func (x *Thompson) Learn(g string, s policy.State, paid bool, at int64) {
	b := x.belief(g+"|"+s.Context["method"], at)
	if paid {
		b.paid++
	} else {
		b.failed++
	}
}

func betaSample(rng *rand.Rand, a, b float64) float64 {
	x := gammaSample(rng, a)
	return x / (x + gammaSample(rng, b))
}

// gammaSample is Marsaglia and Tsang's method, for shape >= 1.
func gammaSample(rng *rand.Rand, shape float64) float64 {
	d := shape - 1.0/3
	c := 1 / math.Sqrt(9*d)
	for {
		z := rng.NormFloat64()
		v := 1 + c*z
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng.Float64()
		if math.Log(u) < 0.5*z*z+d-d*v+d*math.Log(v) {
			return d * v
		}
	}
}
