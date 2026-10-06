module github.com/OwlShack/meshcore-go

go 1.26.7

require (
	filippo.io/edwards25519 v1.2.0
	golang.org/x/crypto v0.56.0
)

retract v1.0.7 // tagged from the wrong commit, then re-tagged; use v1.0.8 instead
