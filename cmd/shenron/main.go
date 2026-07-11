package main

import (
	"os"

	"github.com/S1933/Shenron/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
