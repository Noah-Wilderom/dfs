package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Noah-Wilderom/dfs/pkg/logging"
	"github.com/Noah-Wilderom/dfs/pkg/network"
	"go.uber.org/zap"
)

func main() {
	logger := logging.MustNew()
	defer logger.Sync()

	logger.Info("DFS Daemon starting...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create and configure network
	opts := network.P2PNetworkingOpts{
		Port:           9000,
		EnableDHT:      false, // Start simple - no DHT
		BootstrapPeers: []string{},
		Logger:         logger,
	}

	p2pNet := network.NewP2PNetworking(opts)
	defer p2pNet.Close()

	// Start network
	if err := p2pNet.Start(ctx); err != nil {
		logger.Fatal("Failed to start network", zap.Error(err))
	}

	// Print connection info
	host := p2pNet.Host()
	fmt.Println("\n══════════════════════════════════════")
	fmt.Println("  DFS Node Ready")
	fmt.Println("══════════════════════════════════════")
	fmt.Printf("\nPeer ID: %s\n\n", host.ID().String())
	fmt.Println("Listening on:")
	for _, addr := range host.Addrs() {
		fmt.Printf("  %s/p2p/%s\n", addr, host.ID())
	}
	fmt.Println("\n══════════════════════════════════════\n")

	logger.Info("Daemon ready. Press Ctrl+C to stop.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down...")
}
