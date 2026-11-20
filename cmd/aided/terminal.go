package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

// terminal is where a person answers: stdin when it is one, else /dev/tty
// (under `curl | sh`).
type terminal struct {
	mu  sync.Mutex
	in  *os.File
	r   *bufio.Reader
	out io.Writer
}

func newTerminal() (*terminal, error) {
	in := os.Stdin
	if !term.IsTerminal(int(in.Fd())) {
		tty, err := os.Open("/dev/tty")
		if err != nil {
			return nil, errors.New("this needs a terminal to answer in")
		}
		in = tty
	}
	return &terminal{in: in, r: bufio.NewReader(in), out: os.Stdout}, nil
}

func (t *terminal) line(prompt string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprint(t.out, prompt)
	l, err := t.r.ReadString('\n')
	if err != nil && l == "" {
		return "", err
	}
	return strings.TrimSpace(l), nil
}

func (t *terminal) secret(prompt string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprint(t.out, prompt)
	b, err := term.ReadPassword(int(t.in.Fd()))
	fmt.Fprintln(t.out)
	return string(b), err
}

// startSigner answers the owner role's signing requests while a command
// runs (internal/signer): it shows what each batch signs in this terminal
// and signs with the key its passphrase opens, which stays open only until
