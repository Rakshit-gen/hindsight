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

## License

MIT
