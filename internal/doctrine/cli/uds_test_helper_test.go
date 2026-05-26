package cli

import "net"

func newUDSListener(sockPath string) (net.Listener, error) {
	return net.Listen("unix", sockPath)
}
