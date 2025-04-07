package naive

import "net"

type RoutingEntry struct {
	Subnet *net.IPNet
	PopID  uint16
}
