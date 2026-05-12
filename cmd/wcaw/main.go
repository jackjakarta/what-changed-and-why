package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackjakarta/what-changed-and-why/internal/history"
)

const usage = `usage: wcaw <path>:<symbol>

example:
  wcaw src/auth/login.ts:validateToken
`

func main() {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	path, symbol, err := splitArg(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(2)
	}
	_ = symbol // symbol resolution arrives in Phase 2

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(1)
	}

	commits, err := history.WalkFile(cwd, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(1)
	}

	for _, c := range commits {
		fmt.Printf("%s\t%s\t%s\t%s\n",
			c.Hash[:7],
			c.Date.Format("2006-01-02"),
			c.Author,
			c.Subject,
		)
	}
}

func splitArg(arg string) (path, symbol string, err error) {
	i := strings.LastIndex(arg, ":")
	if i < 0 {
		return "", "", fmt.Errorf("invalid argument %q: expected <path>:<symbol>", arg)
	}
	path, symbol = arg[:i], arg[i+1:]
	if path == "" || symbol == "" {
		return "", "", fmt.Errorf("invalid argument %q: expected <path>:<symbol>", arg)
	}
	return path, symbol, nil
}
