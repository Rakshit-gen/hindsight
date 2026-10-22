# hindsight

Score a payment routing policy on payments another policy routed, before anyone turns it on.

A merchant with more than one payment gateway routes each attempt by some rules: UPI to this gateway, cards from that bank to the other one, retry on the next one down the list. Someone proposes a change. The usual ways to find out if it's better are an A/B test, which loses real payments on the worse arm for as long as it runs, or a backtest on success rates, which can't tell you what would have happened on a gateway the payment never went to.

If the router writes down, when it picks a gateway, the chance it had of picking it, then its logs are enough to estimate how any other policy would have done on the same payments. That's off-policy evaluation. Adyen [described doing this for payments](https://arxiv.org/abs/2501.10470) and found the inverse-propensity estimates lined up with their A/B tests. hindsight is a small tool for doing it on your own logs, with the parts that are specific to payments:

- **The payment is the unit, not the attempt.** What matters is whether the payment got paid in the end, after retries. A change to where retries go doesn't touch a single first attempt, so an estimate built from first attempts can't see it. hindsight weights each payment by the product over its attempts.
- **Paired comparisons.** Two policies scored on the same logs share most of their luck. The difference is estimated payment by payment, which gives a much narrower interval than comparing two separate ones.
- **Diagnostics that say when not to trust it**: effective sample size, the largest weight, whether weights average 1, and how much of the proposed policy's routing goes where the logs never went.
- **Late outcomes.** UPI payments can sit pending for minutes and retries come minutes later, so the newest payments in any log are unfinished. Counting them as failures biases every estimate down. A horizon leaves them out by start time instead.
- **An exploration planner**: how many payments it costs a day to explore, and how many days of those logs it takes to see a change of a given size.

It runs as a command line tool or [in the browser](#in-the-browser), where the logs stay on your machine.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/Rakshit-gen/hindsight/main/scripts/install.sh | sh
```

That downloads the release for your OS and architecture, checks its SHA256, and puts `hindsight` in `~/.local/bin`. Or with Go 1.24 or later:

```sh
go install github.com/Rakshit-gen/hindsight/cmd/hindsight@latest
```

or from a clone, `make install`.

## Use

```sh
# a simulated day of logs to try it on
hindsight sim --days 1 --epsilon 0.1 > logs.jsonl

# what the logs look like: propensities, attempts, pending outcomes
hindsight check --logs logs.jsonl

# score a change against today's rules on those logs
hindsight eval --logs logs.jsonl --policy examples/sbi-cards-to-cedar.json \
  --baseline examples/today.json --horizon 15m

# how much exploring a change of this size needs
hindsight plan --per-day 80000 --share 0.04 --lift 0.15
```

`eval` prints each estimator's paid rate and its difference from the baseline, with 95% intervals, then the diagnostics and any warnings. `--json` prints the same as JSON. `--clip` caps weights, which trades variance for bias toward the logger.

## Logs

One record per attempt, as JSON lines or CSV with a header row.

```json
{"payment":"p1","attempt":1,"at":1767620000000,"context":{"method":"card","issuer":"sbin"},
 "eligible":["alder","birch","cedar"],"gateway":"birch","propensity":0.9333,
 "probs":{"alder":0.0333,"birch":0.9333,"cedar":0.0333},"outcome":"declined","settled_at":1767620030000}
```

| field | |
|---|---|
| `payment` | groups attempts into payments |
| `attempt` | 1, 2, 3 with no gaps |
| `at` | unix milliseconds or RFC 3339 |
| `context` | whatever the router knew: method, bank, card network, amount band. Policies match on these. In CSV, any column not listed here is context. |
| `eligible` | gateways that could take this attempt. In CSV, `alder\|birch`. |
| `gateway` | where it went |
| `propensity` | the chance the router had of picking `gateway`, at that moment |
| `probs` | optional: the router's whole distribution. With it, hindsight can tell exactly where the router could never have gone. In CSV, `alder:0.03\|birch:0.93`. |
| `outcome` | `success`, `pending`, or anything else for a failure |
| `settled_at` | optional: when the outcome became known |

The propensity has to be the one the router used when it chose. A router that picks by rules with a share sent at random knows it exactly. A Thompson sampling router doesn't, by default: it draws one sample and takes the best. The fix is to estimate the win chances first (sample the beliefs a thousand times, count wins, mix in a floor), route by those chances, and log them. Then the propensity is exact by construction, and the chances can be cached for a minute so it isn't slow.

## Policies

Policies are rule tables in JSON, the form most routing configs already take.

```json
{
  "name": "sbi-cards-to-cedar",
  "epsilon": 0,
  "retry": "next",
  "rules": [
    {"when": {"method": "card", "issuer": "sbin"}, "order": ["cedar", "birch", "alder"]},
    {"when": {"method": "card"}, "order": ["birch", "cedar", "alder"]},
    {"when": {"method": "upi", "attempt": "2+"}, "split": {"alder": 1, "cedar": 1}},
    {"order": ["birch", "alder", "cedar"]}
  ]
}
```

The first rule whose `when` matches and that can place the payment does. `order` sends it to the first candidate on the list; `split` spreads it by weight. `when` values can be `a|b`, and the key `attempt` matches the attempt number, with `2+` for any retry. `retry: "next"`, the default, leaves out gateways the payment already tried unless none are left; `"same"` doesn't. `epsilon` sends that share of payments to a random eligible gateway, tried or not; a rule can set its own. A payment no rule places is spread evenly.

The examples in [`examples/`](examples) are written for the simulated world below.

## Estimators

For payment *i* with attempts *t* = 1…*T*, logged propensities μ and target probabilities π, the weight is *w* = ∏ π(*a_t*)/μ(*a_t*) and *y* is 1 if the payment got paid.

| | |
|---|---|
| `first` | *w* from the first attempt only, times *y*: what you get if you treat attempts as independent decisions |
| `ips` | *w* · *y* |
| `snips` | Σ *w y* / Σ *w*, self-normalised |
| `dm` | an outcome model alone: the chance each attempt pays by gateway, first try or retry, and context, each finer level shrunk toward the one above, plus the chance a failed payment is tried again |
| `dr` | sequential doubly robust ([Jiang and Li](https://arxiv.org/abs/1511.03722)): the model's value, corrected attempt by attempt with the running weight |

The model in `dm` and `dr` is cross-fitted: each half of the payments is valued by a model fitted on the other half. Intervals are normal, from the per-payment terms; for `snips` from its linearisation. Differences between two policies, or between a policy and the logs, use per-payment differences of the same terms.

The model knows nothing about time, outages or why an attempt failed, which is how a model fitted to real logs turns out too. `dr` corrects what it gets wrong; `dm` doesn't, and its interval only reflects how payments vary, not how wrong the model is, so it's too narrow. Read `dr` and `snips`.

## Does it get the right answer?

There's no way to check an off-policy estimate on real logs, because the true value of a policy nobody ran is what's unknown. So `hindsight bench` checks it in an invented world (package [`sim`](sim)) where the exact answer can be computed. **Every number in this section comes from that world, not from any real gateway.** Its rates, shares and delays are made up to have the shape of Indian online payments: three gateways (alder, birch, cedar), UPI about 62% of volume, cards 30%, netbanking 8%, a long tail of banks, a few gateway and bank pairings that are much worse than the rest, customers who fail on their own, banks that keep declining a payment whichever gateway it comes through, gateways that keep erroring on the same payment, UPI outcomes that arrive one to ten minutes late, and a 90-minute outage on birch's UPI on the first afternoon. Every policy meets the same customers with the same luck, and a rule table's exact paid rate on a set of payments is computed by following every branch.

Each row below is 20 simulated days of about 60 payments a minute (around 77,000 payments a day), with the last 15 minutes left out. Run it with `hindsight bench`; it takes about four minutes on a laptop. The changes being scored, against today's rules:

- `sbi-cards-to-cedar`: SBI cards go to cedar first. About 4% of payments; birch is bad with SBI cards.
- `upi-to-alder`: UPI goes to alder first. Most payments. Its gain comes from the outage on birch: on the same day without it, the change loses 0.51 points.
- `retry-same-gateway`: retries stay on the gateway that just failed. No first attempt changes.

### Estimators

Logger: today's rules, 10% explored. Points of paid rate.

| change | true difference | estimator | mean estimate | bias | rmse | 95% interval ± | covers truth |
|---|---|---|---|---|---|---|---|
| sbi-cards-to-cedar | +0.21 | first | +0.15 | -0.060 | 0.302 | 0.70 | 95% |
|  |  | ips | +0.15 | -0.061 | 0.301 | 0.70 | 95% |
|  |  | snips | +0.21 | +0.001 | 0.164 | 0.26 | 95% |
|  |  | dm | +0.24 | +0.030 | 0.096 | 0.01 | 0% |
|  |  | dr | +0.21 | +0.000 | 0.171 | 0.26 | 95% |
| upi-to-alder | +1.00 | first | +0.67 | -0.323 | 1.274 | 2.82 | 100% |
|  |  | ips | +0.64 | -0.353 | 1.322 | 2.83 | 100% |
|  |  | snips | +0.79 | -0.211 | 0.459 | 1.17 | 100% |
|  |  | dm | +0.25 | -0.744 | 0.870 | 0.01 | 0% |
|  |  | dr | +0.79 | -0.212 | 0.461 | 1.17 | 100% |
| retry-same-gateway | -2.28 | first | +0.00 | +2.275 | 2.278 | 0.00 | 0% |
|  |  | ips | -2.51 | -0.239 | 0.990 | 1.99 | 70% |
|  |  | snips | -2.71 | -0.431 | 1.606 | 2.83 | 90% |
|  |  | dm | -0.84 | +1.437 | 1.444 | 0.00 | 0% |
|  |  | dr | -2.68 | -0.404 | 1.332 | 2.25 | 95% |

- The first-attempt estimate says keeping retries on the same gateway changes nothing. It costs 2.28 points. That's the case for scoring whole payments.
- The outcome model alone is confident and wrong: its intervals are a hundredth of a point wide and never cover the truth. On the retry change it sees a third of the loss, because it doesn't know a gateway that errored on a payment tends to error again.
- `snips` and `dr` have about half the error of plain `ips` and cover the truth at about the rate they should. Even so, after one day at 10% exploration each interval is about as wide as the effect it measures, which is what the planner below is for.

## License

MIT
