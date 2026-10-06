module github.com/OwlShack/meshcore-go/companion/transport

go 1.26.7

require (
	github.com/OwlShack/meshcore-go v1.6.0
	go.bug.st/serial v1.8.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

replace github.com/OwlShack/meshcore-go => ../../

retract v1.0.7 // tagged from the wrong commit, then re-tagged; use v1.0.8 instead
