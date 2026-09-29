// Package version exposes build metadata for the QuotaNoa client.
package version

// Version is the agent version reported during the protocol handshake.
// It can be overridden at build time with:
//
//	go build -ldflags "-X github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/version.Version=0.2.0"
var Version = "0.1.0"
