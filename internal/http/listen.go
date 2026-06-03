package http

import "net"

func netListen(network, addr string) (net.Listener, error) {
	return net.Listen(network, addr)
}
