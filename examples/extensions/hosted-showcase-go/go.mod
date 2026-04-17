module github.com/dimetron/pi-go/examples/extensions/hosted-showcase-go

go 1.22

require (
	github.com/dimetron/pi-go/pkg/piapi v0.0.0
	github.com/dimetron/pi-go/pkg/piext v0.0.0
)

replace (
	github.com/dimetron/pi-go/pkg/piapi => ../../../pkg/piapi
	github.com/dimetron/pi-go/pkg/piext => ../../../pkg/piext
)
