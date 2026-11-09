package webapps

import "net"

func newLoopbackListener() (net.Listener, error) { return net.Listen("tcp4", "127.0.0.1:0") }
