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
	return nil
}

func (n *P2PNetworking) Close() error {
	if err := n.host.Close(); err != nil {
		return err
	}

	return nil
}

func (n *P2PNetworking) Start(ctx context.Context) error {
	if err := n.prepare(); err != nil {
		n.logger.Error("Error on preparing connection", zap.Error(err))
	}

	for _, addr := range dht.DefaultBootstrapPeers {
		pi, _ := peer.AddrInfoFromP2pAddr(addr)
		// We ignore errors as some bootstrap peers may be down
		// and that is fine.
		n.host.Connect(ctx, *pi)
	}

	return nil
}
