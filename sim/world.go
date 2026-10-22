// Package sim is an invented payments world with known answers, for
// checking whether the estimators get them right.
//
// Every rate, share and delay is made up to have the shape of Indian online
// payments: UPI is most of the volume, cards next, a long tail of banks,
// three gateways that are each a little better at something, a few
// gateway and bank pairings that are much worse, customers who fail on
// their own, banks that keep declining, and UPI outcomes that arrive
// minutes late. None of it is measured from a real gateway. What it gives
// is the exact value of any policy on the same payments, so an estimate
// can be checked against the truth.
package sim

import (
	"math"
	"math/rand/v2"
	"strconv"
	"time"
)

var Gateways = []string{"alder", "birch", "cedar"}

var (
	methods      = []string{"upi", "card", "netbanking"}
	methodWeight = []float64{62, 30, 8}
	banks        = []string{"hdfc", "icic", "sbin", "axis", "kotak", "yesb", "idfc", "barb"}
)

// Epoch is when every simulated log starts: a Monday.
var Epoch = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC).UnixMilli()

const (
	MaxAttempts = 3
	// A failed payment that is tried again goes again this much later.
	RetryGap = 2 * time.Minute
	// Outcomes that aren't pending reach the router this much later.
	Feedback = 30 * time.Second
)

var (
	base  = map[string]float64{"upi": 0.9, "card": 0.91, "netbanking": 0.87}
	skill = map[string]map[string]float64{
		"alder": {"card": 0.98, "upi": 0.975, "netbanking": 0.99},
		"birch": {"card": 1, "upi": 1, "netbanking": 0.97},
		"cedar": {"card": 0.985, "upi": 0.985, "netbanking": 1},
	}
	// The few strong pairings, on top of small random ones everywhere.
	planted = map[string]float64{
		"birch|card|sbin":       0.82,
		"alder|upi|kotak":       0.9,
		"cedar|card|hdfc":       1.03,
		"alder|netbanking|icic": 0.85,
		"cedar|upi|axis":        0.93,
	}
	eligible = map[string][]string{
		"upi":        {"alder", "birch", "cedar"},
		"card":       {"alder", "birch", "cedar"},
		"netbanking": {"alder", "cedar"},
	}
	// Customers fail on their own this often, whichever gateway.
	customer = map[string]float64{"upi": 0.14, "card": 0.11, "netbanking": 0.13}
	// Share of UPI attempts whose outcome comes minutes late.
	pendingShare = map[string]float64{"alder": 0.05, "birch": 0.12, "cedar": 0.03}
	// Chance someone tries again after each kind of failure.
	again = map[string]float64{"customer": 0.35, "declined": 0.5, "error": 0.8}
)

const (
	// A bank that declined a payment usually declines it again, whichever
	// gateway it comes through.
	declineRepeats = 0.6
	// A gateway that errored on a payment often errors on it again.
	errorRepeats = 0.5
	// Of failures that aren't the customer's or a repeat, this share are
	// the bank declining; the rest are gateway errors.
	declineShare = 0.4
)

// Payment is one customer checkout.
type Payment struct {
	I      uint64
	At     int64 // unix ms
	Method string
	Issuer string
}

func (p Payment) ID() string { return "p" + strconv.FormatUint(p.I, 10) }
func (p Payment) Context() map[string]string {
	return map[string]string{"method": p.Method, "issuer": p.Issuer}
}
func (p Payment) Eligible() []string    { return eligible[p.Method] }
func (p Payment) attemptAt(k int) int64 { return p.At + int64(k-1)*RetryGap.Milliseconds() }

// Incident multiplies one gateway's success rate on one method for a while.
type Incident struct {
	Gateway string
	Method  string
	Start   time.Duration // from Epoch
	Length  time.Duration
	Factor  float64
}

type World struct {
	Seed      uint64
	Incidents []Incident
}

// Rate is the chance gateway g completes an attempt once the customer has
// done their part.
func (w *World) Rate(g string, p Payment, at int64) float64 {
	key := g + "|" + p.Method + "|" + p.Issuer
	r := base[p.Method] * skill[g][p.Method] * (1 + 0.03*(2*unit(w.Seed, 1, hashString(key))-1))
	if f, ok := planted[key]; ok {
		r *= f
	}
	for _, x := range w.Incidents {
		t := time.Duration(at-Epoch) * time.Millisecond
		if x.Gateway == g && x.Method == p.Method && t >= x.Start && t < x.Start+x.Length {
			r *= x.Factor
		}
	}
	return min(r, 0.995)
}

// Traffic is Poisson arrivals over some days, busiest mid-afternoon.
func Traffic(seed uint64, days float64, perMinute float64) []Payment {
	rng := rand.New(rand.NewPCG(seed, 11))
	bankW := make([]float64, len(banks))
	for r := range bankW {
		bankW[r] = 1 / math.Pow(float64(r+1), 0.9)
	}
	var out []Payment
	end := days * 86_400_000
	for t, i := 0.0, uint64(0); ; i++ {
		hour := math.Mod(t/3_600_000, 24)
		rate := perMinute * (0.9 + 0.45*math.Sin((hour-9)/24*2*math.Pi))
		t += rng.ExpFloat64() / rate * 60_000
		if t >= end {
			return out
		}
		out = append(out, Payment{I: i, At: Epoch + int64(t), Method: methods[pick(rng.Float64(), methodWeight)], Issuer: banks[pick(rng.Float64(), bankW)]})
	}
}

func pick(u float64, w []float64) int {
	total := 0.0
	for _, x := range w {
		total += x
	}
	u *= total
	for i, x := range w {
		if u -= x; u <= 0 {
			return i
		}
	}
	return len(w) - 1
}
