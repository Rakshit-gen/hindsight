package sim

import (
	"time"

	"github.com/Rakshit-gen/hindsight/policy"
)

// Truth is the exact chance pol gets each payment paid, averaged over the
// world's draws and the policy's own randomness. A policy that only looks
// at the payment can be valued this way; one that learns can't.
func (w *World) Truth(ps []Payment, pol policy.Policy) []float64 {
	out := make([]float64, len(ps))
	memo := map[[2]string]float64{}
	for i, p := range ps {
		// Away from incidents a payment's value depends only on its method
		// and bank.
		key := [2]string{p.Method, p.Issuer}
		quiet := !w.near(p)
		if v, ok := memo[key]; ok && quiet {
			out[i] = v
			continue
		}
		out[i] = w.value(p, pol, 1, history{}, nil)
		if quiet {
			memo[key] = out[i]
		}
	}
	return out
}

func (w *World) near(p Payment) bool {
	for _, x := range w.Incidents {
		t := time.Duration(p.At-Epoch) * time.Millisecond
		if t+MaxAttempts*RetryGap >= x.Start && t < x.Start+x.Length {
			return true
		}
	}
	return false
}

// value follows attempt() branch by branch.
func (w *World) value(p Payment, pol policy.Policy, k int, h history, tried []string) float64 {
	s := policy.State{Context: p.Context(), Attempt: k, Tried: tried, Eligible: p.Eligible()}
	total := 0.0
	for gw, q := range pol.Probs(s) {
		if q == 0 {
			continue
		}
		g := index(gw)
		c := customer[p.Method]
		pd, pe := 0.0, 0.0
		if h.declined {
			pd = declineRepeats
		}
		if h.errored&(1<<g) != 0 {
			pe = errorRepeats
		}
		r := w.Rate(gw, p, p.attemptAt(k))
		v := (1 - c) * (1 - pd) * (1 - pe) * r
		if k < MaxAttempts {
			decl := (1 - c) * (pd + (1-pd)*(1-pe)*(1-r)*declineShare)
			errd := (1 - c) * (1 - pd) * (pe + (1-pe)*(1-r)*(1-declineShare))
			stay := 1.0
			if p.Method == "upi" {
				stay = 1 - pendingShare[gw]
			}
			next := append(tried[:len(tried):len(tried)], gw)
			v += stay * (c*again["customer"]*w.value(p, pol, k+1, h.after(g, "customer"), next) +
				decl*again["declined"]*w.value(p, pol, k+1, h.after(g, "declined"), next) +
				errd*again["error"]*w.value(p, pol, k+1, h.after(g, "error"), next))
		}
		total += q * v
	}
	return total
}
