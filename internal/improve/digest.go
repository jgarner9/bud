package improve

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"jgarner.dev/bud/internal/approval"
)

// Rendered is a digest: its text, the new proposals it shows (a yes opens
// them), and the keys of the regressions and promotions it reports, so the
// next digest doesn't report them again
type Rendered struct {
	Text     string
	Shown    []string
	Reported []string
	// lines are the text each reported key showed as, so a key whose line
	// a cut dropped isn't recorded as reported
	lines map[string]string
}

// Visible keeps the reported keys whose lines are still in text after a
// cut, so a regression or promotion Jason never saw is reported again
func (d Rendered) Visible(text string) []string {
	var out []string
	for _, k := range d.Reported {
		if strings.Contains(text, d.lines[k]) {
			out = append(out, k)
		}
	}
	return out
}

// maxCandidates bounds the landscape lines
const maxCandidates = 8

// maxEvidence bounds one proposal's evidence in the digest; the pull
// request carries it whole
const maxEvidence = 400

// digestBudget leaves room in approval.MaxSummary for the title and expiry
// lines the prompt adds
const digestBudget = approval.MaxSummary - 400

// Render writes the weekly digest from the state. edgeNow is the content
// version edge offers now, which is the only one promote takes
func Render(st *State, edgeNow string) Rendered {
	d := Rendered{lines: map[string]string{}}
	var b strings.Builder
	var news []Proposal
	for _, p := range st.Proposals {
		if p.Status == New {
			news = append(news, p)
		}
	}

	if len(news) > 0 {
		fmt.Fprintf(&b, "New proposals (a yes opens each as a draft pull request for you to review and merge; a no drops them):\n")
		for i, p := range news {
			var e strings.Builder
			where := "public content"
			if p.Target == Overlay {
				where = "the overlay"
			}
			fmt.Fprintf(&e, "\n%d. %s (%s, %s)\n", i+1, p.Title, p.ID, where)
			ev := p.Evidence
			if utf8.RuneCountInString(ev) > maxEvidence {
				ev = string([]rune(ev)[:maxEvidence]) + "..."
			}
			for line := range strings.SplitSeq(ev, "\n") {
				e.WriteString("   " + line + "\n")
			}
			var files []string
			for _, f := range p.Files {
				files = append(files, f.Path)
			}
			fmt.Fprintf(&e, "   Changes %s.\n", strings.Join(files, ", "))
			if len(p.Criteria) > 0 {
				var cs []string
				for _, c := range p.Criteria {
					cs = append(cs, criterionText(c))
				}
				fmt.Fprintf(&e, "   Works if: %s.\n", strings.Join(cs, "; "))
			}
			if b.Len()+e.Len() > digestBudget/2 {
				fmt.Fprintf(&b, "\n...and %d more; bud improve list shows them all.\n", len(news)-i)
				break
			}
			b.WriteString(e.String())
			d.Shown = append(d.Shown, p.ID)
		}
	}

	var regressed, promote, stale []string
	for _, p := range st.Proposals {
		switch {
		case p.Status == Regressed:
			key := "regressed:" + p.ID
			if slices.Contains(st.Digest.Reported, key) {
				continue
			}
			line := fmt.Sprintf("%s (%s): a revert is proposed above or was already", p.Title, p.ID)
			regressed = append(regressed, line)
			d.Reported, d.lines[key] = append(d.Reported, key), line
		case p.Target == Public && (p.Status == Passed || p.Status == Unmeasured) && p.Edge != "" && p.Stable == "" && edgeNow != "":
			key := "promote:" + p.ID + ":" + edgeNow
			if slices.Contains(st.Digest.Reported, key) {
				continue
			}
			how := "held its numbers"
			if p.Status == Unmeasured {
				how = "couldn't be measured (" + p.Note + ")"
			}
			line := fmt.Sprintf("%s (%s) %s on edge.", p.Title, p.ID, how)
			promote = append(promote, line)
			d.Reported, d.lines[key] = append(d.Reported, key), line
		case p.Status == Stale && p.Note != "a newer scan replaced it":
			stale = append(stale, fmt.Sprintf("%s (%s): %s", p.Title, p.ID, p.Note))
		}
	}
	section(&b, "Got worse after merging", regressed)
	if len(promote) > 0 {
		promote = append(promote, fmt.Sprintf("Promote content %s to stable with: gh workflow run content.yml -f mode=promote -f version=%s", edgeNow, edgeNow))
	}
	section(&b, "Ready for stable", promote)
	section(&b, "Couldn't be opened", stale)

	if sc := st.Scan; sc != nil {
		var lines []string
		for i, c := range sc.Candidates {
			if i == maxCandidates {
				lines = append(lines, fmt.Sprintf("...and %d more new models", len(sc.Candidates)-i))
				break
			}
			lines = append(lines, candidateText(c))
		}
		if len(sc.Gone) > 0 {
			lines = append(lines, "No longer listed, so take them out of their roles: "+strings.Join(sc.Gone, ", "))
		}
		for _, r := range sc.Runtimes {
			switch {
			case r.Error != "":
				lines = append(lines, fmt.Sprintf("%s: couldn't read its newest release (%s)", r.Name, r.Error))
			case r.Newer:
				lines = append(lines, fmt.Sprintf("%s has a newer release, %s; bud pins %s", r.Name, r.Latest, r.Pinned))
			}
		}
		for _, f := range sc.Failed {
			lines = append(lines, "Couldn't read the model list of "+f)
		}
		section(&b, "This week's landscape scan ("+sc.At.Format("2006-01-02")+")", lines)
	}

	d.Text = strings.TrimSpace(b.String())
	if utf8.RuneCountInString(d.Text) > digestBudget {
		d.Text = string([]rune(d.Text)[:digestBudget]) + "\n...cut; bud improve show has the whole digest."
	}
	d.Reported = d.Visible(d.Text)

	return d
}

func section(b *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(title + ":")
	for _, l := range lines {
		b.WriteString("\n- " + l)
	}
}

func criterionText(c Criterion) string {
	unit := " a day"
	if strings.HasPrefix(c.Metric, "doctor.") {
		unit = ""
	}
	s := fmt.Sprintf("%s%s no more than", c.Metric, unit)
	switch {
	case c.MaxRise == 0 && c.Slack == 0:
		return s + " the week before"
	case c.MaxRise == 0:
		return fmt.Sprintf("%s the week before plus %s", s, num(c.Metric, c.Slack))
	default:
		return fmt.Sprintf("%s %.0f%% over the week before plus %s", s, c.MaxRise*100, num(c.Metric, c.Slack))
	}
}

func candidateText(c Candidate) string {
	s := c.Provider + "/" + c.Name
	var facts []string
	if c.Context > 0 {
		facts = append(facts, fmt.Sprintf("%dk context", c.Context/1000))
	}
	if c.Price != nil {
		facts = append(facts, fmt.Sprintf("$%g/$%g per M tokens", c.Price.In, c.Price.Out))
	}
	if len(facts) > 0 {
		s += " (" + strings.Join(facts, ", ") + ")"
	}
	switch {
	case c.Unknown:
		s += ": the provider doesn't say what it can do"
	case len(c.Roles) > 0:
		s += ": could fill " + strings.Join(c.Roles, ", ")
	default:
		s += ": meets no role's needs"
	}
	return s
}

// Body is the pull request's description
func Body(p Proposal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Proposed by `bud improve` (%s, Suite D47, D125 to D129). Nothing applies it until you merge it; Claude threads don't merge `%s` pull requests.\n\n", p.ID, Label)
	if p.Reverts != "" {
		fmt.Fprintf(&b, "This reverts %s.\n\n", p.Reverts)
	}
	fmt.Fprintf(&b, "## What was seen\n\n%s\n\n", p.Evidence)
	b.WriteString("## How to tell it worked\n\n")
	if len(p.Criteria) == 0 {
		b.WriteString("No criteria, so the loop won't check it after the merge.\n\n")
	} else {
		for _, c := range p.Criteria {
			fmt.Fprintf(&b, "- %s\n", criterionText(c))
		}
		b.WriteString("\nThe loop compares the week before the merge with the week after and proposes a revert if any of these got worse.\n\n")
	}
	b.WriteString("## Rollout\n\n")
	if p.Target == Overlay {
		b.WriteString("This is the overlay, which has no channels: once merged it reaches every device at its next `bud sync`.\n")
	} else {
		b.WriteString("Merging cuts a content release on the edge channel. Once the check passes, the weekly digest names the command that promotes it to stable. Recovery is `bud sync --rollback` on a device, or the revert the loop proposes.\n")
	}

	return b.String()
}
