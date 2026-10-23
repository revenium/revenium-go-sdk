package sandbox

import (
	"context"
	"fmt"
	"net"

	"github.com/revenium/revenium-go-sdk/core/internal/netdial"
)

// dial catches the clients that build their own transport and reach a host
// without a DNS lookup: an IP literal, or a name the hosts file maps. It
// checks the address it is about to connect to, so it also covers a proxy
// the transport connects to instead of the host. The transport dials on a
// goroutine of its own, so the attempt cannot name the test that made it.
func (g *guard) dial(next netdial.DialFunc) netdial.DialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if err := g.checkDestination(ctx, host, address); err != nil {
			return nil, err
		}
		return next(ctx, network, address)
	}
}

func (g *guard) checkDestination(ctx context.Context, host, address string) error {
	if isLocal(host) {
		return nil
	}
	if net.ParseIP(host) != nil {
		return g.blockConnection(address, address)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	for _, addr := range addrs {
		if !addr.IP.IsLoopback() {
			return g.blockConnection(address, fmt.Sprintf("%s (%s)", address, addr.IP))
		}
	}
	return nil
}

func (g *guard) blockConnection(address, described string) error {
	g.record(fmt.Sprintf("a connection to %s (the dialer cannot tell which test asked; look at the test running when this line was printed)", described))
	return fmt.Errorf("%w (connection to %s)", errBlocked, address)
}
