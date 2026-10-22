package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/Rakshit-gen/hindsight/examples"
	"github.com/Rakshit-gen/hindsight/logs"
	"github.com/Rakshit-gen/hindsight/ope"
	"github.com/Rakshit-gen/hindsight/plan"
	"github.com/Rakshit-gen/hindsight/policy"
	"github.com/Rakshit-gen/hindsight/sim"
)

// The bench runs the estimators on many simulated logs and checks them
// against the exact answers. Everything it prints comes from the invented
// world in package sim.

var tables = []string{"accuracy", "loggers", "propensities", "budget", "paired", "horizon"}

func benchCmd(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	seeds := fs.Int("seeds", 20, "simulated logs per row")
	days := fs.Float64("days", 1, "days of traffic in each log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	b := bench{seeds: *seeds, days: *days}
	run := fs.Args()
	if len(run) == 0 {
		run = tables
	}
	for _, name := range run {
		if !slices.Contains(tables, name) {
			return fmt.Errorf("no table %q; there are %v", name, tables)
		}
	}
	fmt.Printf("Invented world, %d logs per row, %.3g days of about 60 payments a minute each.\n", b.seeds, b.days)
	start := time.Now()
	for _, name := range run {
		map[string]func(){
			"accuracy": b.accuracy, "loggers": b.loggers, "propensities": b.propensities, "budget": b.budget, "paired": b.paired, "horizon": b.horizon,
		}[name]()
	}
	fmt.Fprintf(os.Stderr, "bench took %s\n", time.Since(start).Round(time.Second))
	return nil
}

type bench struct {
	seeds int
	days  float64
}

const perMinute = 60

// trial is one simulated log and what's needed to know the truth on it.
type trial struct {
	w      *sim.World
	ps     []sim.Payment
	logged []logs.Payment
}

func (b bench) play(seed uint64, days float64, lg sim.Logger, guess bool, horizon time.Duration) trial {
	w := &sim.World{Seed: seed, Incidents: sim.Incidents()}
	ps := sim.Traffic(seed, days, perMinute)
	as := sim.Play(w, ps, lg, seed, sim.Epoch+int64(days*86_400_000))
	if guess {
		as = sim.Guessed(as)
	}
	ls, err := logs.Group(as)
	if err != nil {
		panic(err)
	}
	ls, _ = ope.Settled(ls, horizon)
	return trial{w, ps, ls}
}

// truth is pol's exact paid rate on the payments the estimate used.
func (t trial) truth(pol policy.Policy) float64 {
	vs := t.w.Truth(t.ps, pol)
	at := make(map[string]int, len(t.ps))
	for i, p := range t.ps {
		at[p.ID()] = i
	}
	s := 0.0
	for _, p := range t.logged {
		s += vs[at[p.ID]]
	}
	return s / float64(len(t.logged))
}

// each runs f for every seed, a few at a time.
func (b bench) each(f func(seed uint64)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(1, runtime.NumCPU()/2))
	for s := range b.seeds {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			f(uint64(1000 + s))
		}()
	}
	wg.Wait()
}

// tally collects estimates of one quantity against its truth.
type tally struct {
	mu                          sync.Mutex
	n, covered                  int
	err, err2, half, est, true_ float64
}

func (t *tally) add(e ope.Estimate, truth float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	d := e.Value - truth
	t.n++
	t.err += d
	t.err2 += d * d
	t.half += 1.96 * e.SE
	t.est += e.Value
	t.true_ += truth
	if math.Abs(d) <= 1.96*e.SE {
		t.covered++
	}
}

func (t *tally) bias() float64     { return 100 * t.err / float64(t.n) }
func (t *tally) rmse() float64     { return 100 * math.Sqrt(t.err2/float64(t.n)) }
func (t *tally) width() float64    { return 100 * t.half / float64(t.n) }
func (t *tally) coverage() float64 { return 100 * float64(t.covered) / float64(t.n) }
func (t *tally) truth() float64    { return 100 * t.true_ / float64(t.n) }
func (t *tally) mean() float64     { return 100 * t.est / float64(t.n) }

const horizon = 15 * time.Minute

func explored(name string, eps float64) *policy.Table {
	t := *examples.Load(name)
	t.Epsilon = eps
	return &t
}

func (b bench) accuracy() {
	fmt.Println("\n## accuracy")
	fmt.Println("\nThe logger runs today's rules and explores 10% of payments. Each change is")
	fmt.Println("scored against today's rules on the same logs; points of paid rate.")
	fmt.Println()
	fmt.Println("| change | true difference | estimator | mean estimate | bias | rmse | 95% interval ± | covers truth |")
	fmt.Println("|---|---|---|---|---|---|---|---|")
	targets := []string{"sbi-cards-to-cedar", "upi-to-alder", "retry-same-gateway"}
	today := examples.Load("today")
	got := map[string]*tally{}
	for _, t := range targets {
		for _, e := range ope.Names {
			got[t+e] = &tally{}
		}
	}
	b.each(func(seed uint64) {
		tr := b.play(seed, b.days, sim.Static{Policy: explored("today", 0.1)}, false, horizon)
		base := tr.truth(today)
		for _, name := range targets {
			target := examples.Load(name)
			r, err := ope.Evaluate(tr.logged, target, today, ope.Options{})
			if err != nil {
				panic(err)
			}
			truth := tr.truth(target) - base
			for _, row := range r.Rows {
				got[name+row.Name].add(row.Diff, truth)
			}
		}
	})
	for _, t := range targets {
		for i, e := range ope.Names {
			g := got[t+e]
			label, truth := "", ""
			if i == 0 {
				label, truth = t, fmt.Sprintf("%+.2f", g.truth())
			}
			fmt.Printf("| %s | %s | %s | %+.2f | %+.3f | %.3f | %.2f | %.0f%% |\n", label, truth, e, g.mean(), g.bias(), g.rmse(), g.width(), g.coverage())
		}
	}
}

type loggerCase struct {
	name  string
	make  func(seed uint64) sim.Logger
	guess bool
}

func (b bench) loggers() {
	fmt.Println("\n## loggers")
	fmt.Println("\nThe same change, SBI cards to cedar, scored against today's rules on logs from")
	fmt.Println("different live routers. Estimates are dr; points of paid rate.")
	fmt.Println()
	fmt.Println("| logger | its paid rate | true difference | bias | rmse | 95% interval ± | covers truth | effective sample | out of reach |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|")
	static := func(eps float64) func(uint64) sim.Logger {
		return func(uint64) sim.Logger { return sim.Static{Policy: explored("today", eps)} }
	}
	thompson := func(seed uint64) sim.Logger {
		return &sim.Thompson{Floor: 0.05, HalfLife: 2 * time.Hour, Draws: 1000, Seed: seed}
	}
	cases := []loggerCase{
		{"today's rules, no exploring", static(0), false},
		{"today's rules, 1% explored", static(0.01), false},
		{"today's rules, 5% explored", static(0.05), false},
		{"today's rules, 10% explored", static(0.1), false},
		{"sliding window, 5% explored", func(uint64) sim.Logger { return &sim.Window{Size: 300, Epsilon: 0.05} }, false},
		{"Thompson, 5% floor, logged propensities", thompson, false},
	}
	today, target := examples.Load("today"), examples.Load("sbi-cards-to-cedar")
	for _, c := range cases {
		var g tally
		var mu sync.Mutex
		var paid, ess, deficit float64
		b.each(func(seed uint64) {
			tr := b.play(seed, b.days, c.make(seed), c.guess, horizon)
			r, err := ope.Evaluate(tr.logged, target, today, ope.Options{})
			if err != nil {
				panic(err)
			}
			g.add(r.Rows[4].Diff, tr.truth(target)-tr.truth(today))
			mu.Lock()
			paid += r.Logged.Value
			ess += r.Diag.ESS / float64(r.Payments)
			deficit += r.Diag.Deficit
			mu.Unlock()
		})
		n := float64(b.seeds)
		fmt.Printf("| %s | %.2f%% | %+.2f | %+.3f | %.3f | %.2f | %.0f%% | %.1f%% | %.2f%% |\n",
			c.name, 100*paid/n, g.truth(), g.bias(), g.rmse(), g.width(), g.coverage(), 100*ess/n, 100*deficit/n)
	}
}

func (b bench) propensities() {
	fmt.Println("\n## propensities")
	fmt.Println("\nToday's rules send all UPI to birch. Scored on logs from routers that moved UPI")
	fmt.Println("off birch during the outage, with the propensities they logged and with ones")
	fmt.Println("guessed afterwards from how often each gateway was picked. dr; points.")
	fmt.Println()
	fmt.Println("| logger | propensities | true paid rate | mean estimate | bias | rmse | covers truth |")
	fmt.Println("|---|---|---|---|---|---|---|")
	today := examples.Load("today")
	for _, kind := range []string{"window", "thompson"} {
		for _, guess := range []bool{false, true} {
			var g tally
			b.each(func(seed uint64) {
				lg, _ := sim.NewLogger(kind, 0.05, nil, seed)
				tr := b.play(seed, b.days, lg, guess, horizon)
				s, err := ope.Score(tr.logged, today, ope.Options{})
				if err != nil {
					panic(err)
				}
				g.add(s.Of("dr"), tr.truth(today))
			})
			how := "logged"
			if guess {
				how = "guessed later"
			}
			name := map[string]string{"window": "sliding window, 5% explored", "thompson": "Thompson, 5% floor"}[kind]
			fmt.Printf("| %s | %s | %.2f%% | %.2f%% | %+.3f | %.3f | %.0f%% |\n", name, how, g.truth(), g.mean(), g.bias(), g.rmse(), g.coverage())
		}
	}
}

func avg(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func sd(xs []float64) float64 {
	m, v := avg(xs), 0.0
	for _, x := range xs {
		v += (x - m) * (x - m)
	}
	return math.Sqrt(v / float64(len(xs)-1))
}
