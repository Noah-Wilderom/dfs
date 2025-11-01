package network

import (
	"context"
	"fmt"
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
	host   host.Host
	dht    *dht.IpfsDHT
	logger *zap.Logger

	peersMu sync.RWMutex
	peers   map[peer.ID]peer.AddrInfo

	P2PNetworkingOpts
}

type P2PNetworkingOpts struct {
	Port           int
	EnableDHT      bool
	BootstrapPeers []string
	Logger         *zap.Logger
}

func NewP2PNetworking(opts P2PNetworkingOpts) *P2PNetworking {
	if opts.Port == 0 {
		opts.Port = 9000
	}

	return &P2PNetworking{
		logger:            opts.Logger,
		peers:             make(map[peer.ID]peer.AddrInfo),
		P2PNetworkingOpts: opts,
	}
}

func (n *P2PNetworking) Start(ctx context.Context) error {
	// Generate identity
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		return err
	}

	// Connection manager
	connManager, err := connmgr.NewConnManager(100, 400, connmgr.WithGracePeriod(time.Minute))
	if err != nil {
		return err
	}

	listenAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", n.Port))

	// Build options
	libp2pOpts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrs(listenAddr),
		libp2p.Security(libp2ptls.ID, libp2ptls.New),
		libp2p.Security(noise.ID, noise.New),
		libp2p.DefaultTransports,
		libp2p.ConnectionManager(connManager),
		libp2p.NATPortMap(),
	}

	// Add DHT if enabled
	if n.EnableDHT {
		libp2pOpts = append(libp2pOpts, libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			n.dht, err = dht.New(ctx, h)
			return n.dht, err
		}))
	}

	// Create host
	h, err := libp2p.New(libp2pOpts...)
	if err != nil {
		return err
	}
	n.host = h

	// Setup notifications
	h.Network().Notify(&networkNotifiee{net: n, logger: n.logger})

	n.logger.Info("P2P Node Ready",
		zap.String("PeerID", h.ID().String()),
		zap.Strings("Addresses", formatAddrs(h.Addrs())),
	)

	// Bootstrap DHT if enabled
	if n.EnableDHT && len(n.BootstrapPeers) > 0 {
		n.bootstrapDHT(ctx, n.BootstrapPeers)
	}

	return nil
}

func (n *P2PNetworking) bootstrapDHT(ctx context.Context, bootstrapPeers []string) {
	if len(bootstrapPeers) == 0 {
		n.logger.Info("No bootstrap peers, skipping DHT bootstrap")
		return
	}

	n.logger.Info("Bootstrapping DHT...", zap.Int("peers", len(bootstrapPeers)))

	for _, peerStr := range bootstrapPeers {
		addr, _ := multiaddr.NewMultiaddr(peerStr)
		pi, _ := peer.AddrInfoFromP2pAddr(addr)

		if err := n.host.Connect(ctx, *pi); err == nil {
			n.logger.Info("Connected to bootstrap peer", zap.String("peer", pi.ID.String()))
		}
	}

	if n.dht != nil {
		n.dht.Bootstrap(ctx)
	}
}

func (n *P2PNetworking) Host() host.Host {
	return n.host
}

func (n *P2PNetworking) Close() error {
	if n.dht != nil {
		n.dht.Close()
	}
	if n.host != nil {
		return n.host.Close()
	}
	return nil
}

// Network notifications
type networkNotifiee struct {
	net    *P2PNetworking
	logger *zap.Logger
}

func (nn *networkNotifiee) Connected(net network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()
	nn.net.peersMu.Lock()
	nn.net.peers[peerID] = peer.AddrInfo{ID: peerID, Addrs: []multiaddr.Multiaddr{conn.RemoteMultiaddr()}}
	nn.net.peersMu.Unlock()

	nn.logger.Info("Peer connected", zap.String("peer", peerID.String()))
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

func formatAddrs(addrs []multiaddr.Multiaddr) []string {
	formatted := make([]string, len(addrs))
	for i, addr := range addrs {
		formatted[i] = addr.String()
	}
	return formatted
}
