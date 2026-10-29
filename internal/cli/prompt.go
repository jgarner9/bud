package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// prompter asks questions on a terminal. Every question has a default, which an
// empty answer or end of input picks
type prompter struct {
	in  *bufio.Reader
	out io.Writer
}

func newPrompter(in io.Reader, out io.Writer) *prompter {
	return &prompter{in: bufio.NewReader(in), out: out}
}

// terminalPrompter asks on the terminal, or is nil when there's nobody to ask:
// with --yes, or when stdin isn't a terminal. A nil prompter takes the defaults
func terminalPrompter(yes bool) *prompter {
	if yes || !isTerminal(os.Stdin) {
		return nil
	}

	return newPrompter(os.Stdin, os.Stdout)
}

// errNotConfirmed is returned when the user answers no; the command did nothing
var errNotConfirmed = errors.New("nothing changed")

// confirmOnTerminal asks question before something destructive. --yes skips
// it; with no terminal to ask on it refuses, and retry says how to skip asking
func confirmOnTerminal(question string, yes bool, retry string) error {
	if yes {
		return nil
	}
	if !isTerminal(os.Stdin) {
		return fmt.Errorf("nothing changed: there's no terminal to confirm on, run '%s' to skip asking", retry)
	}
	ok, err := newPrompter(os.Stdin, os.Stdout).confirm(question)
	if err != nil {
		return err
	}
	if !ok {
		return errNotConfirmed
	}

	return nil
}

// ask prints question with its default and returns the trimmed answer
func (p *prompter) ask(question, def string) (string, error) {
	if def == "" {
		fmt.Fprintf(p.out, "%s: ", question)
	} else {
		fmt.Fprintf(p.out, "%s [%s]: ", question, def)
	}

	line, err := p.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if errors.Is(err, io.EOF) {
		// keep the next output off the prompt line
		fmt.Fprintln(p.out)
	}
	if line = strings.TrimSpace(line); line == "" {
		return def, nil
	}

	return line, nil
}

// maxTries is how many invalid answers a question takes before giving up
const maxTries = 3

// choose asks until the answer is one of options
func (p *prompter) choose(question string, options []string, def string) (string, error) {
	q := fmt.Sprintf("%s (%s)", question, strings.Join(options, ", "))
	for range maxTries {
		answer, err := p.ask(q, def)
		if err != nil {
			return "", err
		}
		if slices.Contains(options, answer) {
			return answer, nil
		}
		fmt.Fprintf(p.out, "%q isn't one of %s\n", answer, strings.Join(options, ", "))
	}

	return "", fmt.Errorf("no valid answer to %q", question)
}

// chooseMany asks for a space or comma separated list drawn from options, or
// "all" for every option
func (p *prompter) chooseMany(question string, options, def []string) ([]string, error) {
	q := fmt.Sprintf("%s (any of: %s, or all)", question, strings.Join(options, " "))
	defLabel := strings.Join(def, " ")
	if isAll(def, options) {
		defLabel = "all"
	}

	for range maxTries {
		answer, err := p.ask(q, defLabel)
		if err != nil {
			return nil, err
		}

		picked := strings.FieldsFunc(answer, func(r rune) bool { return r == ' ' || r == ',' })
		if slices.ContainsFunc(picked, func(s string) bool { return strings.EqualFold(s, "all") }) {
			return slices.Clone(options), nil
		}
		var unknown []string
		for _, s := range picked {
			if !slices.Contains(options, s) {
				unknown = append(unknown, s)
			}
		}
		if len(unknown) == 0 && len(picked) > 0 {
			// in the options' order, without repeats
			var out []string
			for _, o := range options {
				if slices.Contains(picked, o) {
					out = append(out, o)
				}
			}
			return out, nil
		}
		if len(unknown) > 0 {
			fmt.Fprintf(p.out, "unknown: %s\n", strings.Join(unknown, ", "))
		}
	}

	return nil, fmt.Errorf("no valid answer to %q", question)
}

// isAll reports whether picked holds every option
func isAll(picked, options []string) bool {
	for _, o := range options {
		if !slices.Contains(picked, o) {
			return false
		}
	}

	return len(options) > 0
}

// confirm asks a yes/no question that defaults to no
func (p *prompter) confirm(question string) (bool, error) {
	answer, err := p.ask(question+" [y/N]", "")
	if err != nil {
		return false, err
	}

	switch strings.ToLower(answer) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// confirmYes asks a yes/no question that defaults to yes
func (p *prompter) confirmYes(question string) (bool, error) {
	answer, err := p.ask(question+" [Y/n]", "")
	if err != nil {
		return false, err
	}

	switch strings.ToLower(answer) {
	case "n", "no":
		return false, nil
	default:
		return true, nil
	}
}
