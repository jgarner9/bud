package improve

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Snapshot is one day's measurements on the home server: the router's
// spend so far this month (cumulative, as its ledger keeps it) and the
// server's doctor counts
type Snapshot struct {
	// Day is the UTC day, as 2026-11-22
	Day        string             `json:"day"`
	Month      string             `json:"month"`
	Spend      map[string]float64 `json:"spend"`
	TopUp      float64            `json:"topup"`
	DoctorFail int                `json:"doctor_fail"`
	DoctorWarn int                `json:"doctor_warn"`
	// NoSpend is true when the router's ledger couldn't be read, so the
	// day has no spend numbers
	NoSpend bool `json:"no_spend,omitempty"`
}

// keepDays bounds the snapshots kept
const keepDays = 60

// Record keeps s as its day's snapshot, replacing one taken earlier that
// day, and drops snapshots older than keepDays
func (st *State) Record(s Snapshot, now time.Time) {
	out := st.Snapshots[:0]
	cut := now.UTC().AddDate(0, 0, -keepDays).Format(time.DateOnly)
	for _, old := range st.Snapshots {
		if old.Day != s.Day && old.Day >= cut {
			out = append(out, old)
		}
	}
	out = append(out, s)
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	st.Snapshots = out
}

// daily is the metric's value on each day that has one. Spend is a day's
// change in the month's total, so a day needs the day before's snapshot
// (or to be the month's first); doctor counts are the day's level
func (st *State) daily(metric string) map[string]float64 {
	out := map[string]float64{}
	for i, s := range st.Snapshots {
		if strings.HasPrefix(metric, "doctor.") {
			if metric == "doctor.fail" {
				out[s.Day] = float64(s.DoctorFail)
			} else {
				out[s.Day] = float64(s.DoctorWarn)
			}
			continue
		}
		if s.NoSpend {
			continue
		}
		var prev *Snapshot
		if i > 0 {
			prev = &st.Snapshots[i-1]
		}
		day, err := time.Parse(time.DateOnly, s.Day)
		if err != nil {
			continue
		}
		var base float64
		switch {
		case prev != nil && !prev.NoSpend && prev.Month == s.Month && prev.Day == day.AddDate(0, 0, -1).Format(time.DateOnly):
			base = spendOf(*prev, metric)
		case day.Day() == 1 || prev != nil && prev.Month != s.Month && prev.Day == day.AddDate(0, 0, -1).Format(time.DateOnly):
			// a new month starts from nothing
		default:
			continue
		}
		out[s.Day] = math.Max(0, spendOf(s, metric)-base)
	}

	return out
}

func spendOf(s Snapshot, metric string) float64 {
	switch {
	case metric == "spend.topup":
		return s.TopUp
	case metric == "spend.total":
		var t float64
		for _, v := range s.Spend {
			t += v
		}
		return t
	default:
		return s.Spend[strings.TrimPrefix(metric, "spend.model:")]
	}
}

// Window is how long before and after a merge a check compares
const Window = 7 * 24 * time.Hour

// minDays is how many days of numbers each side of a check needs
const minDays = 3

// Result is one criterion's before and after
type Result struct {
	Metric string  `json:"metric"`
	Before float64 `json:"before"`
	After  float64 `json:"after"`
	Limit  float64 `json:"limit"`
	Worse  bool    `json:"worse"`
}

func (r Result) String() string {
	verdict := "held"
	if r.Worse {
		verdict = "got worse"
	}
	return fmt.Sprintf("%s %s: %s a day before, %s after (limit %s)", r.Metric, verdict, num(r.Metric, r.Before), num(r.Metric, r.After), num(r.Metric, r.Limit))
}

func num(metric string, v float64) string {
	if strings.HasPrefix(metric, "spend.") {
		return fmt.Sprintf("$%.2f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

// Compare checks each criterion over the Window before the merge and the
// Window after it (the merge day itself in neither). ok is false while
// either side has fewer than minDays of numbers
func (st *State) Compare(criteria []Criterion, merged time.Time) (results []Result, ok bool) {
	mergeDay := merged.UTC().Format(time.DateOnly)
	from := merged.UTC().Add(-Window).Format(time.DateOnly)
	to := merged.UTC().Add(Window).Format(time.DateOnly)
	for _, c := range criteria {
		var before, after []float64
		for day, v := range st.daily(c.Metric) {
			switch {
			case day >= from && day < mergeDay:
				before = append(before, v)
			case day > mergeDay && day <= to:
				after = append(after, v)
			}
		}
		if len(before) < minDays || len(after) < minDays {
			return nil, false
		}
		r := Result{Metric: c.Metric, Before: mean(before), After: mean(after)}
		r.Limit = r.Before*(1+c.MaxRise) + c.Slack
		// a hair over the limit is rounding, not a regression
		r.Worse = r.After > r.Limit+1e-9
		results = append(results, r)
	}

	return results, true
}

func mean(xs []float64) float64 {
	var t float64
	for _, x := range xs {
		t += x
	}
	return t / float64(len(xs))
}
