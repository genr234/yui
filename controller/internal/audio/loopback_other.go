//go:build !windows || !(amd64 || arm64)

package audio

import "context"

func (p *ProcessLoopback) start(context.Context) (<-chan Chunk, error) {
	return nil, ErrLoopbackUnsupported
}
