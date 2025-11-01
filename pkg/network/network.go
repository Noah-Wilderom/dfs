package network

import "context"

type Networking interface {
	Close() error
	Start(context.Context) error
}
