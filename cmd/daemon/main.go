package main

import (
	"time"

	"github.com/Noah-Wilderom/dfs/pkg/logging"
)

var (
	logger = logging.MustNew()
)

func main() {
	logger.Info("Daemon started...")
	for {
		time.Sleep(5 * time.Second)
	}
}
