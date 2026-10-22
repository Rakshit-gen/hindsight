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
