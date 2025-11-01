package network

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
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

	peersMu sync.RWMutex
	peers   map[peer.ID]peer.AddrInfo

	P2PNetworkingOpts
}

type P2PNetworkingOpts struct {
	Logger         *zap.Logger
	BootstrapPeers []string
}

func NewP2PNetworking(opts P2PNetworkingOpts) *P2PNetworking {
	return &P2PNetworking{
		P2PNetworkingOpts: opts,
		peers:             make(map[peer.ID]peer.AddrInfo),
	}
}

func (n *P2PNetworking) Start(ctx context.Context) error {
	// Generate or load identity
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Connection manager to limit connections
	connManager, err := connmgr.NewConnManager(
		100, // Low water mark
		400, // High water mark
		connmgr.WithGracePeriod(time.Minute),
	)
	if err != nil {
		return fmt.Errorf("failed to create connection manager: %w", err)
	}

	portStr := os.Getenv("DFS_PORT")
	if portStr == "" {
		portStr = "9000"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return err
	}

	listenAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port))
	if err != nil {
		return fmt.Errorf("failed to create listen address: %w", err)
	}

	// Build libp2p options
	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrs(listenAddr),
		libp2p.Security(libp2ptls.ID, libp2ptls.New),
		libp2p.Security(noise.ID, noise.New),
		libp2p.DefaultTransports,
		libp2p.ConnectionManager(connManager),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			n.dht, err = dht.New(ctx, h)
			return n.dht, err
		}),
	}

	// Create the host
	h, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}
	n.host = h

	// Set up network notifications
	h.Network().Notify(&networkNotifiee{
		net:    n,
		logger: n.Logger,
	})

	n.Logger.Info("P2P Host created",
		zap.String("ID", h.ID().String()),
		zap.Strings("Addresses", formatAddrs(h.Addrs())),
	)

	if err := n.bootstrapDHT(ctx); err != nil {
		n.Logger.Warn("DHT bootstrap failed, continuing anyway", zap.Error(err))
	}

	n.Logger.Info("Network is ready",
		zap.Int("bootstrap_peers", len(n.BootstrapPeers)),
	)

	return nil
}

func (n *P2PNetworking) bootstrapDHT(ctx context.Context) error {
	n.Logger.Info("Bootstrapping DHT...")

	// Determine which bootstrap peers to use
	var bootstrapPeers []multiaddr.Multiaddr

	if len(n.BootstrapPeers) > 0 {
		// Use custom bootstrap peers
		n.Logger.Info("Using custom bootstrap peers", zap.Int("count", len(n.BootstrapPeers)))
		for _, peerStr := range n.BootstrapPeers {
			addr, err := multiaddr.NewMultiaddr(peerStr)
			if err != nil {
				n.Logger.Warn("Invalid bootstrap peer", zap.String("peer", peerStr), zap.Error(err))
				continue
			}
			bootstrapPeers = append(bootstrapPeers, addr)
		}
	} else {
		// Use IPFS default bootstrap peers
		n.Logger.Info("Using IPFS default bootstrap peers")
		bootstrapPeers = dht.DefaultBootstrapPeers
	}

	// Connect to bootstrap peers
	connectedCount := 0
	for _, addr := range bootstrapPeers {
		pi, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			n.Logger.Warn("Failed to parse bootstrap peer", zap.String("addr", addr.String()), zap.Error(err))
			continue
		}

		if err := n.host.Connect(ctx, *pi); err != nil {
			n.Logger.Debug("Failed to connect to bootstrap peer",
				zap.String("peer", pi.ID.String()),
				zap.Error(err))
		} else {
			connectedCount++
			n.Logger.Info("Connected to bootstrap peer", zap.String("peer", pi.ID.String()))
		}
	}

	if connectedCount == 0 {
		return fmt.Errorf("failed to connect to any bootstrap peers")
	}

	n.Logger.Info("Successfully bootstrapped", zap.Int("peers", connectedCount))

	// Bootstrap the DHT
	if err := n.dht.Bootstrap(ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	n.Logger.Info("DHT bootstrap complete")
	return nil
}

func (n *P2PNetworking) ConnectPeer(ctx context.Context, addrStr string) error {
	addr, err := multiaddr.NewMultiaddr(addrStr)
	if err != nil {
		return fmt.Errorf("invalid multiaddr: %w", err)
	}

	pi, err := peer.AddrInfoFromP2pAddr(addr)
	if err != nil {
		return fmt.Errorf("failed to parse peer info: %w", err)
	}

	n.Logger.Info("Connecting to peer", zap.String("peer", pi.ID.String()))

	if err := n.host.Connect(ctx, *pi); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	n.Logger.Info("Successfully connected to peer", zap.String("peer", pi.ID.String()))
	return nil
}

func (n *P2PNetworking) GetPeers() []peer.AddrInfo {
	n.peersMu.RLock()
	defer n.peersMu.RUnlock()

	peers := make([]peer.AddrInfo, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	return peers
}
func (n *P2PNetworking) Host() host.Host {
	return n.host
}

func (n *P2PNetworking) DHT() *dht.IpfsDHT {
	return n.dht
}

func (n *P2PNetworking) Close() error {
	if n.dht != nil {
		if err := n.dht.Close(); err != nil {
			n.Logger.Warn("Error closing DHT", zap.Error(err))
		}
	}

	if n.host != nil {
		if err := n.host.Close(); err != nil {
			return fmt.Errorf("failed to close host: %w", err)
		}
	}

	n.Logger.Info("P2P network closed")
	return nil
}

// Network event notifications
type networkNotifiee struct {
	net    *P2PNetworking
	logger *zap.Logger
}

func (nn *networkNotifiee) Connected(net network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()

	nn.net.peersMu.Lock()
	nn.net.peers[peerID] = peer.AddrInfo{
		ID:    peerID,
		Addrs: []multiaddr.Multiaddr{conn.RemoteMultiaddr()},
	}
	nn.net.peersMu.Unlock()

	nn.logger.Info("Peer connected",
		zap.String("peer", peerID.String()),
		zap.String("addr", conn.RemoteMultiaddr().String()),
	)
}

func (nn *networkNotifiee) Disconnected(net network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()

	nn.net.peersMu.Lock()
	delete(nn.net.peers, peerID)
	nn.net.peersMu.Unlock()

	nn.logger.Info("Peer disconnected", zap.String("peer", peerID.String()))
}

func (nn *networkNotifiee) Listen(network.Network, multiaddr.Multiaddr)      {}
func (nn *networkNotifiee) ListenClose(network.Network, multiaddr.Multiaddr) {}

// Helper function to format addresses for logging
func formatAddrs(addrs []multiaddr.Multiaddr) []string {
	formatted := make([]string, len(addrs))
	for i, addr := range addrs {
		formatted[i] = addr.String()
	}
	return formatted
}
