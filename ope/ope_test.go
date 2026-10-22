package ope

import (
	"math"
	"testing"
	"time"

	"github.com/Rakshit-gen/hindsight/examples"
	"github.com/Rakshit-gen/hindsight/logs"
	"github.com/Rakshit-gen/hindsight/policy"
	"github.com/Rakshit-gen/hindsight/sim"
)

func played(t *testing.T, seed uint64, logger policy.Policy, days float64) (*sim.World, []sim.Payment, []logs.Payment) {
	t.Helper()
	w := &sim.World{Seed: seed}
	ps := sim.Traffic(seed, days, 30)
	ls, err := logs.Group(sim.Play(w, ps, sim.Static{Policy: logger}, seed, math.MaxInt64))
	if err != nil {
		t.Fatal(err)
	}
	return w, ps, ls
}

func truth(w *sim.World, ps []sim.Payment, pol policy.Policy) float64 {
	s := 0.0
	for _, v := range w.Truth(ps, pol) {
		s += v
	}
	return s / float64(len(ps))
}

func explore(name string, eps float64) *policy.Table {
	t := *examples.Load(name)
	t.Epsilon = eps
	return &t
}

func TestEstimatorsFindTheTruth(t *testing.T) {
	logger := explore("today", 0.6)
	w, ps, ls := played(t, 2, logger, 1)
	for _, name := range []string{"sbi-cards-to-cedar", "upi-to-alder", "retry-same-gateway"} {
		target := examples.Load(name)
		want := truth(w, ps, target)
		s, err := Score(ls, target, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, est := range []string{"ips", "snips", "dr"} {
			e := s.Of(est)
			if math.Abs(e.Value-want) > 3.5*e.SE {
				t.Errorf("%s %s: %.4f ± %.4f, truth %.4f", name, est, e.Value, e.SE, want)
			}
		}
		if mw := s.Diag.MeanWeight; math.Abs(mw-1) > 4*s.Diag.MeanWeightSE {
			t.Errorf("%s: mean weight %.3f ± %.3f", name, mw, s.Diag.MeanWeightSE)
		}
		if s.Diag.Deficit != 0 {
			t.Errorf("%s: deficit %v with full exploration", name, s.Diag.Deficit)
		}
	}
}

// Moving retries can't be seen from first attempts alone.
func TestFirstAttemptMissesRetries(t *testing.T) {
	logger := explore("today", 0.6)
	_, _, ls := played(t, 3, logger, 1)
	s, _ := Score(ls, examples.Load("retry-same-gateway"), Options{})
	base, _ := Score(ls, examples.Load("today"), Options{})
	if d := Diff(s.Cols["first"], base.Cols["first"]); d.Value != 0 {
		t.Errorf("first-attempt estimate should not move, moved %v", d.Value)
	}
	if d := Diff(s.Cols["dr"], base.Cols["dr"]); d.Value > -3*d.SE {
		t.Errorf("payment-level estimate should see the loss: %v ± %v", d.Value, d.SE)
	}
}

func TestDeficit(t *testing.T) {
	_, _, ls := played(t, 5, examples.Load("today"), 0.3)
	s, _ := Score(ls, examples.Load("upi-to-alder"), Options{})
	// UPI first attempts are most attempts, and the logger never left
	// birch for them.
	if s.Diag.Deficit < 0.4 || len(s.Diag.Warnings) == 0 {
		t.Errorf("deficit %v, warnings %v", s.Diag.Deficit, s.Diag.Warnings)
	}
	same, _ := Score(ls, examples.Load("today"), Options{})
	if same.Diag.Deficit != 0 || same.Of("ips").Value != same.Of("logged").Value {
		t.Errorf("scoring the logger on its own logs should give back what happened")
	}
}
