package main

import (
	"os"

	"argocd-app-diff/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
