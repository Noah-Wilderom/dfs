package main

import (
	"github.com/Noah-Wilderom/dfs/pkg/logging"
)

var (
	logger = logging.MustNew()
)

func main() {
	logger.Info("Distributed File System CLI")
}
