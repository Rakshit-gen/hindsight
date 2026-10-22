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
