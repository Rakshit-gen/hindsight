package sim

import (
	"container/heap"
	"maps"
	"math/rand/v2"

	"github.com/Rakshit-gen/hindsight/logs"
	"github.com/Rakshit-gen/hindsight/policy"
)

// Play routes the payments through a live logger and returns its logs as
// they'd read at end: outcomes that haven't arrived yet say pending, and
// retries that would come after end never happen.
func Play(w *World, ps []Payment, lg Logger, seed uint64, end int64) []logs.Attempt {
	rng := rand.New(rand.NewPCG(seed, 3))
	q := &queue{}
	for i, p := range ps {
		q.push(event{at: p.At, p: i, k: 1})
	}
	var out []logs.Attempt
	for q.Len() > 0 {
		e := heap.Pop(q).(event)
		if e.learn {
			lg.Learn(e.gateway, e.state, e.paid, e.at)
			continue
		}
		p := ps[e.p]
		s := policy.State{Context: p.Context(), Attempt: e.k, Tried: e.tried, Eligible: p.Eligible()}
		probs := lg.Probs(s, e.at)
		chosen, u := s.Eligible[len(s.Eligible)-1], rng.Float64()
		for _, g := range s.Eligible {
			if u -= probs[g]; u < 0 {
				chosen = g
				break
			}
		}
		g := index(chosen)
		o := w.attempt(p, e.k, g, e.h)
		a := logs.Attempt{
			Payment: p.ID(), Attempt: e.k, At: e.at, Context: s.Context, Eligible: s.Eligible,
			Gateway: chosen, Propensity: probs[chosen], Probs: maps.Clone(probs), Outcome: "pending",
		}
		known := e.at + o.Delay.Milliseconds()
		if known <= end {
			a.Outcome, a.SettledAt = o.Reason, known
		}
		out = append(out, a)
		q.push(event{at: known, learn: true, gateway: chosen, state: s, paid: o.Reason == "success"})
		if o.Again && e.k < MaxAttempts {
			if next := p.attemptAt(e.k + 1); next <= end {
				tried := append(e.tried[:len(e.tried):len(e.tried)], chosen)
				q.push(event{at: next, p: e.p, k: e.k + 1, h: e.h.after(g, o.Reason), tried: tried})
			}
		}
	}
	return out
}

type event struct {
	at  int64
	seq int

	p     int
	k     int
	h     history
	tried []string

	learn   bool
	gateway string
	state   policy.State
	paid    bool
}

type queue struct {
	es  []event
	seq int
}

func (q *queue) push(e event) {
	q.seq++
	e.seq = q.seq
	heap.Push(q, e)
}

func (q *queue) Len() int { return len(q.es) }
func (q *queue) Less(i, j int) bool {
	a, b := q.es[i], q.es[j]
	if a.at != b.at {
		return a.at < b.at
	}
	return a.seq < b.seq
}
func (q *queue) Swap(i, j int) { q.es[i], q.es[j] = q.es[j], q.es[i] }
func (q *queue) Push(x any)    { q.es = append(q.es, x.(event)) }
func (q *queue) Pop() any {
	e := q.es[len(q.es)-1]
	q.es = q.es[:len(q.es)-1]
	return e
}
