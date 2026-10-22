package sim

import (
	"math"
	"testing"
	"time"

	"github.com/Rakshit-gen/hindsight/examples"
	"github.com/Rakshit-gen/hindsight/logs"
)

// The exact value has to match what playing the world gives, or every
// check against it is meaningless.
func TestTruthMatchesPlay(t *testing.T) {
	w := World{Incidents: []Incident{{Gateway: "birch", Method: "upi", Start: 2 * time.Hour, Length: time.Hour, Factor: 0.4}}}
	ps := Traffic(4, 0.25, 30)
	for _, name := range []string{"today", "retry-same-gateway"} {
		pol := examples.Load(name)
		// One world's draws are fixed per payment, so play many worlds and
		// compare the average of their exact values with what happened.
		truth, played, n := 0.0, 0.0, 0
		for seed := range uint64(40) {
			w2 := &World{Seed: 1000 + seed, Incidents: w.Incidents}
			payments, err := logs.Group(Play(w2, ps, Static{pol}, seed, math.MaxInt64))
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range payments {
				if p.Paid() {
					played++
				}
				n++
			}
			for _, v := range w2.Truth(ps, pol) {
				truth += v
			}
		}
		truth /= float64(n)
		got := played / float64(n)
		se := math.Sqrt(truth * (1 - truth) / float64(n))
		if math.Abs(got-truth) > 4*se {
			t.Errorf("%s: played %.4f, exact %.4f (se %.4f)", name, got, truth, se)
		}
	}
}
