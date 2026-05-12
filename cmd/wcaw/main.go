package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackjakarta/what-changed-and-why/internal/history"
	"github.com/jackjakarta/what-changed-and-why/internal/locator"
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

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(1)
	}

	resolved, err := history.Resolve(cwd, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(1)
	}

	if ext := strings.ToLower(filepath.Ext(resolved.AbsPath)); ext != ".ts" {
		fmt.Fprintf(os.Stderr, "wcaw: unsupported file extension %q: only .ts is supported in v1\n", ext)
		os.Exit(1)
	}

	source, err := os.ReadFile(resolved.AbsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: read file: %v\n", err)
		os.Exit(1)
	}

	sym, err := locator.Locate(source, symbol)
	if err != nil {
		var nfe *locator.NotFoundError
		if errors.As(err, &nfe) {
			fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("resolved %s at %s:%d-%d (bytes %d-%d)\n\n",
		sym.Name, resolved.RelPath, sym.StartLine, sym.EndLine, sym.StartByte, sym.EndByte)

	commits, err := history.WalkResolved(resolved)
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
