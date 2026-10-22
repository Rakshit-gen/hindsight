// Package plan works out what exploration costs and what it buys: how many
// payments a router gives up by sending some at random, and how many days
// of those logs it takes to tell whether a change is better.
//
// The arithmetic is for the simplest case: the logger sends a share
// epsilon of payments to a uniformly random gateway, the change being
// tested is a fixed rule, and only first attempts count. Retries make
// weights multiply, so treat the days here as a floor.
package plan

import "math"

type Inputs struct {
	PerDay   float64 `json:"per_day"`  // payments a day
	Rate     float64 `json:"rate"`     // share paid now
	Gateways int     `json:"gateways"` // gateways a payment can go to
	// Share of payments the change sends somewhere else, and how much
	// likelier those get paid, as fractions: 0.04 and 0.15 for a change
	// that moves 4% of payments and gains 15 points on them.
	Share float64 `json:"share"`
	Lift  float64 `json:"lift"`
	// How much less likely a payment sent at random gets paid than one
	// sent where the logger wanted.
	Gap float64 `json:"gap"`
}

type Row struct {
	Epsilon float64 `json:"epsilon"`
	// Exploring only the payments the change touches, not all of them.
	Targeted bool `json:"targeted"`
	// Payments not paid each day because of exploring.
	LostPerDay float64 `json:"lost_per_day"`
	// Standard error, in points of paid rate, of the estimated difference
	// from one day of logs.
	SEPerDay float64 `json:"se_per_day"`
	// Days of logs to detect the change at 5% significance with 80% power.
	Days float64 `json:"days"`
	Lost float64 `json:"lost"` // LostPerDay times Days
}

// z for a two-sided 5% test plus z for 80% power.
const zz = 1.959964 + 0.841621

// Table gives a row for each epsilon, explored everywhere and only where
// the change acts.
func Table(in Inputs, eps []float64) []Row {
	k := float64(in.Gateways)
	var rows []Row
	for _, e := range eps {
		for _, targeted := range []bool{false, true} {
			// A payment's term in the difference estimate is
			// y * (1[logger chose the target's gateway] / mu - 1). Its
			// variance is about rate * (1 - mu) / mu, where mu is the
			// logger's chance of the target's choice.
			rare := e / k
			v := in.Share * in.Rate * (1 - rare) / rare
			lost := in.PerDay * e * (k - 1) / k * in.Gap
			if targeted {
				lost *= in.Share
			} else {
				usual := 1 - e + e/k
				v += (1 - in.Share) * in.Rate * (1 - usual) / usual
			}
			effect := in.Share * in.Lift
			days := zz * zz * v / (effect * effect) / in.PerDay
			rows = append(rows, Row{
				Epsilon: e, Targeted: targeted, LostPerDay: lost,
				SEPerDay: math.Sqrt(v / in.PerDay), Days: days, Lost: lost * days,
			})
		}
	}
	return rows
}
