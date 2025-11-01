package main

import (
	"context"

	"github.com/Noah-Wilderom/dfs/pkg/logging"
	"github.com/Noah-Wilderom/dfs/pkg/network"
	"go.uber.org/zap"
)

var (
	logger = logging.MustNew()
)

func main() {
	logger.Info("Daemon started...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := network.NewP2PNetworking(logger)
	defer server.Close()
	
	if err := server.Start(ctx); err != nil {
		logger.Error("Error on starting P2PNetworking", zap.Error(err))
	}
}
