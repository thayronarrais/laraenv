// Package version exposes the app version at runtime. The default value is
// overwritten at build time via:
//
//	go build -ldflags "-X lara4ever/internal/version.Version=0.1.0"
package version

var Version = "0.0.0-dev"
