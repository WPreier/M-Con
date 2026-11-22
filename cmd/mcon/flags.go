package main

import (
	"flag"
	"io"
)

func flagSet(stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("mcon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}
