module github.com/pizzaface/go-pi/internal/extension/testdata/hosted-surface-fixture

go 1.22

require (
	github.com/pizzaface/go-pi/pkg/piapi v0.0.0
	github.com/pizzaface/go-pi/pkg/piext v0.0.0
)

replace (
	github.com/pizzaface/go-pi/pkg/piapi => ../../../../pkg/piapi
	github.com/pizzaface/go-pi/pkg/piext => ../../../../pkg/piext
)
