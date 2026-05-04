package main

import (
	"os"

	"github.com/LogicMonitor-IT/n8nctl/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
