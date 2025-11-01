package network

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
)

type P2PNetworking struct {
	host host.Host
	dht  *dht.IpfsDHT

	logger *zap.Logger
}

func NewP2PNetworking(logger *zap.Logger) *P2PNetworking {
	return &P2PNetworking{
		logger: logger,
	}
}

func (n *P2PNetworking) prepare() error {
	priv, _, err := crypto.GenerateKeyPair(
		crypto.Ed25519,
		-1,
	)
	if err != nil {
		return err
	}

	var idht *dht.IpfsDHT

	connManager, err := connmgr.NewConnManager(
		100, // Lowwater
		400, // Highwater
		connmgr.WithGracePeriod(time.Minute),
	)
	if err != nil {
		return err
	}

	portStr := os.Getenv("DFS_PORT")
	if portStr == "" {
		portStr = "9000"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return err
	}
	sourceMultiAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port))

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrs(sourceMultiAddr),
		// support TLS connections
		libp2p.Security(libp2ptls.ID, libp2ptls.New),
		// support noise connections
		libp2p.Security(noise.ID, noise.New),
		libp2p.DefaultTransports,
		libp2p.ConnectionManager(connManager),
		libp2p.NATPortMap(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			idht, err = dht.New(context.Background(), h)
			return idht, err
		}),
		libp2p.EnableNATService(),
	)
	if err != nil {
		return err
	}

	n.host = h
	n.dht = idht
	return nil
}

func (n *P2PNetworking) Close() error {
	if err := n.host.Close(); err != nil {
		return err
	}

	return nil
}

func formatAddrs(addrs []multiaddr.Multiaddr) []string {
	formatted := make([]string, len(addrs))
	for i, addr := range addrs {
		formatted[i] = addr.String()
	}
	return formatted
}

func (n *P2PNetworking) Start(ctx context.Context) error {
	if err := n.prepare(); err != nil {
		n.logger.Error("Error on preparing connection", zap.Error(err))
	}

	n.logger.Info("P2P Host created",
		zap.String("ID", n.host.ID().String()),
		zap.Strings("Addresses", formatAddrs(n.host.Addrs())),
	)

	if err := n.bootstrapDHT(ctx); err != nil {
		n.logger.Error("Error on bootstrap DHT", zap.Error(err))
		return err
	}

	n.setupPeerDiscovery(ctx)
	go n.discoverPeers(ctx)

	n.logger.Info("Network is ready. Waiting for connections...")
	<-ctx.Done()
	return nil
}

func (n *P2PNetworking) bootstrapDHT(ctx context.Context) error {
	n.logger.Info("Bootstrapping DHT...")

	// Connect to bootstrap nodes
	var connectedCount int
	for _, addr := range dht.DefaultBootstrapPeers {
		pi, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			n.logger.Warn("Invalid bootstrap peer", zap.Error(err))
			continue
		}

		if err := n.host.Connect(ctx, *pi); err != nil {
			n.logger.Warn("Failed to connect to bootstrap peer",
				zap.String("peer", pi.ID.String()),
				zap.Error(err),
			)
			continue
		}

		connectedCount++
		n.logger.Info("Connected to bootstrap peer",
			zap.String("peer", pi.ID.String()),
		)
	}

	if connectedCount == 0 {
		return fmt.Errorf("failed to connect to any bootstrap peers")
	}

	// Bootstrap the DHT itself
	if err := n.dht.Bootstrap(ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	n.logger.Info("Successfully bootstrapped", zap.Int("peers", connectedCount))
	return nil
}

func (n *P2PNetworking) setupPeerDiscovery(ctx context.Context) {
	// Monitor peer connections
	n.host.Network().Notify(&NetworkNotifee{
		logger: n.logger,
		host:   n.host,
	})

	n.logger.Info("Peer discovery enabled")
}

func (n *P2PNetworking) discoverPeers(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			peers := n.host.Network().Peers()
			n.logger.Info("Peer status",
				zap.Int("connected", len(peers)),
			)

			if len(peers) > 0 {
				peerIDs := make([]string, len(peers))
				for i, p := range peers {
					peerIDs[i] = p.String()
				}
				n.logger.Debug("Connected peers", zap.Strings("peers", peerIDs))
			}
		}
	}
}

type NetworkNotifee struct {
	logger *zap.Logger
	host   host.Host
}

func (n *NetworkNotifee) Listen(network.Network, multiaddr.Multiaddr)      {}
func (n *NetworkNotifee) ListenClose(network.Network, multiaddr.Multiaddr) {}

func (n *NetworkNotifee) Connected(net network.Network, conn network.Conn) {
	n.logger.Info("Peer connected",
		zap.String("peer", conn.RemotePeer().String()),
		zap.String("addr", conn.RemoteMultiaddr().String()),
	)
}

func (n *NetworkNotifee) Disconnected(net network.Network, conn network.Conn) {
	n.logger.Info("Peer disconnected",
		zap.String("peer", conn.RemotePeer().String()),
	)
}
