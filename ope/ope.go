// Package ope estimates how a routing policy would have done on payments
// another policy routed.
//
// The unit is the payment, not the attempt. A payment that fails on its
// first gateway is often retried on another, and what a merchant cares
// about is whether it got paid in the end. Changing where retries go
// changes that without touching any first attempt, and changing where first
// attempts go changes which payments get retried at all. So each payment's
// weight is the product, over its attempts, of how much likelier the target
// was than the logger to make each choice.
package ope

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Rakshit-gen/hindsight/logs"
	"github.com/Rakshit-gen/hindsight/policy"
)

type Options struct {
	// Caps a payment's weight; 0 leaves weights alone. A cap cuts variance
	// and biases the estimate toward the logger.
	Clip float64
	// Context keys for the outcome model, coarse to fine. Empty picks them
	// from the logs.
	Keys []string
}

// The estimators, in the order reports list them.
//
//	first  reweights only the first attempt, as if retries didn't exist
//	ips    reweights the whole payment
//	snips  ips divided by the mean weight
//	dm     the outcome model alone
//	dr     the model, corrected attempt by attempt with the weights
var Names = []string{"first", "ips", "snips", "dm", "dr"}

// Scores holds each payment's contribution to each estimate. The mean of a
// column is the estimate, so two policies scored on the same logs can be
// compared payment by payment.
type Scores struct {
	Cols map[string][]float64 // Names, plus "logged" for what happened
	Diag Diagnostics
}

type Diagnostics struct {
	// (sum of weights)^2 / sum of squared weights: roughly how many
	// payments the estimate is really built on.
	ESS          float64 `json:"ess"`
	MeanWeight   float64 `json:"mean_weight"`
	MeanWeightSE float64 `json:"mean_weight_se"`
	MaxWeight    float64 `json:"max_weight"`
	MaxShare     float64 `json:"max_share"` // the biggest weight's share of all weight
	// Average over first attempts of the chance the target picks something
	// other than what the logger picked.
	Changed float64 `json:"changed"`
	// Share of the target's choices, over logged attempts, on gateways the
	// logger could never have picked. Estimates can't see those.
	Deficit float64 `json:"deficit"`
	// Share of logged attempts where that can't be told: propensity below
	// 1 and no probs logged.
	Unknown  float64  `json:"unknown"`
	Pending  int      `json:"pending"`
	Warnings []string `json:"warnings,omitempty"`
}

// Score scores the target on the payments.
func Score(ps []logs.Payment, target policy.Policy, opt Options) (*Scores, error) {
	n := len(ps)
	if n == 0 {
		return nil, errors.New("no payments to score")
	}
	keys := opt.Keys
	if len(keys) == 0 {
		keys = pickKeys(ps)
	}
	// Cross-fitting: each half of the payments is valued by a model fitted
	// on the other half, so the model can't memorise the outcomes it is
	// then asked to correct.
	var halves [2][]logs.Payment
	for i, p := range ps {
		halves[i%2] = append(halves[i%2], p)
	}
	vals := [2]*valuer{}
	for f := range vals {
		vals[f] = &valuer{m: fit(halves[f], keys), pol: target, memo: map[string]float64{}}
	}

	s := &Scores{Cols: map[string][]float64{}}
	for _, name := range append([]string{"logged"}, Names...) {
		s.Cols[name] = make([]float64, n)
	}
	weights := make([]float64, n)
	var attempts, deficit, unknown, changed float64
	for i, p := range ps {
		v := vals[1-i%2]
		gws := make([]string, len(p.Attempts))
		states := make([]policy.State, len(p.Attempts))
		for t, a := range p.Attempts {
			gws[t] = a.Gateway
			states[t] = policy.State{Context: a.Context, Attempt: t + 1, Tried: gws[:t:t], Eligible: a.Eligible}
		}
		w, first := 1.0, 0.0
		dr := v.V(states[0])
		for t, a := range p.Attempts {
			probs := target.Probs(states[t])
			w *= probs[a.Gateway] / a.Propensity
			if opt.Clip > 0 && w > opt.Clip {
				w = opt.Clip
			}
			if t == 0 {
				first = w
				changed += 1 - probs[a.Gateway]
			}
			r, next := 0.0, 0.0
			if a.Paid() {
				r = 1
			}
			if t+1 < len(p.Attempts) {
				next = v.V(states[t+1])
			}
			dr += w * (r + next - v.Q(states[t], a.Gateway))

			attempts++
			switch {
			case a.Probs != nil:
				for g, q := range probs {
					if a.Probs[g] <= 0 {
						deficit += q
					}
				}
			case a.Propensity >= 1-1e-9:
				deficit += 1 - probs[a.Gateway]
			default:
				unknown++
			}
			if a.Pending() {
				s.Diag.Pending++
			}
		}
		y := 0.0
		if p.Paid() {
			y = 1
		}
		weights[i] = w
		s.Cols["logged"][i] = y
		s.Cols["first"][i] = first * y
		s.Cols["ips"][i] = w * y
		s.Cols["dm"][i] = v.V(states[0])
		s.Cols["dr"][i] = dr
	}

	// Self-normalised IPS isn't a mean of per-payment terms, so its column
	// holds the linearised terms instead: their mean is the estimate and
	// their spread gives its standard error.
	sw, swy, sw2 := 0.0, 0.0, 0.0
	for i, w := range weights {
		sw += w
		sw2 += w * w
		swy += s.Cols["ips"][i]
		s.Diag.MaxWeight = max(s.Diag.MaxWeight, w)
	}
	if sw > 0 {
		val, wbar := swy/sw, sw/float64(n)
		for i, w := range weights {
			s.Cols["snips"][i] = val + (s.Cols["ips"][i]-val*w)/wbar
		}
		s.Diag.ESS = sw * sw / sw2
		s.Diag.MaxShare = s.Diag.MaxWeight / sw
	}
	mw := mean(weights)
	s.Diag.MeanWeight, s.Diag.MeanWeightSE = mw.Value, mw.SE
	s.Diag.Changed = changed / float64(n)
	s.Diag.Deficit = deficit / attempts
	s.Diag.Unknown = unknown / attempts
	s.Diag.Warnings = warnings(s.Diag, n)
	return s, nil
}

func warnings(d Diagnostics, n int) []string {
	var out []string
	if d.ESS == 0 {
		return append(out, "every weight is zero: the logs never made a choice this policy would make")
	}
	if d.Deficit > 0.001 {
		out = append(out, fmt.Sprintf("%.1f%% of this policy's choices go where the logger never could; every estimate but dm leaves those payments out and reads low", 100*d.Deficit))
	}
	if d.Unknown > 0 && d.Deficit == 0 {
		out = append(out, "propensities below 1 with no probs logged: support can't be checked; log probs to check it")
	}
	if d.ESS < 0.05*float64(n) || d.ESS < 100 {
		out = append(out, fmt.Sprintf("effective sample size is %.0f of %d payments: the estimate rests on a few heavy weights, and its interval is likely too narrow", d.ESS, n))
	}
	if d.MaxShare > 0.05 {
		out = append(out, fmt.Sprintf("one payment carries %.0f%% of all weight", 100*d.MaxShare))
	}
	if math.Abs(d.MeanWeight-1) > max(0.02, 3*d.MeanWeightSE) {
		out = append(out, fmt.Sprintf("weights average %.3f, not 1: propensities may be wrong, or the policy goes where the logs don't", d.MeanWeight))
	}
	if d.Pending > 0 {
		out = append(out, fmt.Sprintf("%d attempts are still pending and count as failures; leave recent payments out with a horizon", d.Pending))
	}
	return out
}

// Estimate is a mean with its standard error.
type Estimate struct {
	Value float64 `json:"value"`
	SE    float64 `json:"se"`
}

// CI is the 95% normal interval.
func (e Estimate) CI() (lo, hi float64) { return e.Value - 1.96*e.SE, e.Value + 1.96*e.SE }

func mean(xs []float64) Estimate {
	n := float64(len(xs))
	s, s2 := 0.0, 0.0
	for _, x := range xs {
		s += x
		s2 += x * x
	}
	m := s / n
	v := (s2/n - m*m) * n / max(n-1, 1)
	return Estimate{m, math.Sqrt(max(v, 0) / n)}
}

// Of returns one estimator's estimate.
func (s *Scores) Of(name string) Estimate { return mean(s.Cols[name]) }

// Diff estimates a minus b payment by payment. Both ran on the same
// payments, so the luck they share cancels, and the interval is much
// narrower than two separate intervals suggest.
func Diff(a, b []float64) Estimate {
	d := make([]float64, len(a))
	for i := range a {
		d[i] = a[i] - b[i]
	}
	return mean(d)
}
