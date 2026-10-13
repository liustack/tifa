module example.com/apitest

go 1.27.0

require github.com/liustack/tifa v0.0.0

require (
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
)

replace github.com/liustack/tifa => ../../..
