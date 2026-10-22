package plan

import (
	"math"
	"math/rand/v2"
	"testing"
)

// The variance formula against a direct simulation of the case it
// describes.
func TestSEMatchesSimulation(t *testing.T) {
	in := Inputs{PerDay: 20_000, Rate: 0.8, Gateways: 3, Share: 0.1, Lift: 0.1, Gap: 0.02}
	rng := rand.New(rand.NewPCG(1, 2))
	for _, row := range Table(in, []float64{0.05, 0.2}) {
		e, k := row.Epsilon, 3
		var ds []float64
		for range 30 {
			sum := 0.0
			for range int(in.PerDay) {
				changed := rng.Float64() < in.Share
				// Gateway 0 is the logger's usual one; the change moves
				// payments to gateway 1.
				a := 0
				if (!row.Targeted || changed) && rng.Float64() < e {
					a = rng.IntN(k)
				}
				target, mu := 0, 1.0
				if changed {
					target = 1
				}
				if !row.Targeted || changed {
					mu = e / float64(k)
					if target == 0 {
						mu += 1 - e
					}
				}
				y := 0.0
				if rng.Float64() < in.Rate {
					y = 1
				}
				hit := 0.0
				if a == target {
					hit = 1
				}
				sum += y * (hit/mu - 1)
			}
			ds = append(ds, sum/in.PerDay)
		}
		m, v := 0.0, 0.0
		for _, d := range ds {
			m += d / float64(len(ds))
		}
		for _, d := range ds {
			v += (d - m) * (d - m) / float64(len(ds)-1)
		}
		if got := math.Sqrt(v); math.Abs(got-row.SEPerDay)/row.SEPerDay > 0.3 {
			t.Errorf("eps %v targeted %v: simulated se %.4f, formula %.4f", e, row.Targeted, got, row.SEPerDay)
		}
	}
}
