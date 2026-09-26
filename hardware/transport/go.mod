module github.com/meshcore-go/meshcore-go/hardware/transport

go 1.26.7

require (
	github.com/meshcore-go/meshcore-go v1.6.0
	go.bug.st/serial v1.8.0
)

require golang.org/x/sys v0.47.0 // indirect

replace github.com/meshcore-go/meshcore-go => ../../

retract v1.0.7 // tagged from the wrong commit, then re-tagged; use v1.0.8 instead
