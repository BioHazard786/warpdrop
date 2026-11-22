// Package version holds the server's version, shown in the web app's About page and `warpdrop config set server`.
package version

// Version is set at build time (the Dockerfile passes the release tag):
//
//	go build -ldflags="-X github.com/BioHazard786/Warpdrop/backend/internal/version.Version=2.0.3"
var Version = "dev"
