package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// IOStreams bundles the CLI's input and output and knows whether they are
// terminals. Tests construct it directly with buffers.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	stdinTTY  bool
	stdoutTTY bool
	color     bool
	noInput   bool

	// readPassword reads a line without echo; nil means "read a plain line".
	readPassword func() (string, error)
	inBuf        *bufio.Reader
}

func systemIO() *IOStreams {
	inTTY := term.IsTerminal(int(os.Stdin.Fd()))
	outTTY := term.IsTerminal(int(os.Stdout.Fd()))
	s := &IOStreams{
		In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr,
		stdinTTY: inTTY, stdoutTTY: outTTY,
		color: outTTY && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb",
	}
	if inTTY {
		s.readPassword = func() (string, error) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(b), err
		}
	}
	return s
}

// IsStdoutTTY reports whether output goes to a terminal (human formatting).
func (s *IOStreams) IsStdoutTTY() bool { return s.stdoutTTY }

// CanPrompt reports whether interactive prompts are allowed.
func (s *IOStreams) CanPrompt() bool {
	return s.stdinTTY && s.stdoutTTY && !s.noInput
}

func (s *IOStreams) reader() *bufio.Reader {
	if s.inBuf == nil {
		s.inBuf = bufio.NewReader(s.In)
	}
	return s.inBuf
}

// ReadLine reads one line from stdin (without the newline).
func (s *IOStreams) ReadLine() (string, error) {
	line, err := s.reader().ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		if errors.Is(err, io.EOF) {
			return "", usageErr("expected input on stdin, got nothing")
		}
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (s *IOStreams) requirePrompt(what, flag string) error {
	if s.CanPrompt() {
		return nil
	}
	return &Error{Code: CodeInputRequired, Exit: ExitUsage,
		Message: fmt.Sprintf("%s is required; pass %s (prompts are disabled when not running in a terminal)", what, flag)}
}

// Prompt asks for a line of text. def is used when the answer is empty.
func (s *IOStreams) Prompt(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(s.ErrOut, "? %s (%s): ", label, def)
	} else {
		fmt.Fprintf(s.ErrOut, "? %s: ", label)
	}
	ans, err := s.ReadLine()
	if err != nil {
		return "", err
	}
	if ans = strings.TrimSpace(ans); ans == "" {
		return def, nil
	}
	return ans, nil
}

// PromptPassword asks for a secret without echoing it.
func (s *IOStreams) PromptPassword(label string) (string, error) {
	fmt.Fprintf(s.ErrOut, "? %s: ", label)
	if s.readPassword != nil {
		return s.readPassword()
	}
	return s.ReadLine()
}

// Confirm asks a yes/no question.
func (s *IOStreams) Confirm(label string, def bool) (bool, error) {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	ans, err := s.Prompt(label+" ["+hint+"]", "")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(ans) {
	case "":
		return def, nil
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// Colors (no-ops when color is off).

func (s *IOStreams) paint(code, text string) string {
	if !s.color {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s *IOStreams) Green(t string) string { return s.paint("32", t) }
func (s *IOStreams) Red(t string) string   { return s.paint("31", t) }
func (s *IOStreams) Yellow(t string) string {
	return s.paint("33", t)
}
func (s *IOStreams) Dim(t string) string  { return s.paint("2", t) }
func (s *IOStreams) Bold(t string) string { return s.paint("1", t) }

// SuccessIcon is a green check mark.
func (s *IOStreams) SuccessIcon() string { return s.Green("✓") }

// Progressf prints a progress note on stderr when stdout is a terminal
// (so scripts and agents reading JSON don't see it).
func (s *IOStreams) Progressf(format string, args ...any) {
	if s.stdoutTTY {
		fmt.Fprintf(s.ErrOut, format+"\n", args...)
	}
}
