package main

import (
	"github.com/dstrop/thumper"
	"github.com/roadrunner-server/roadrunner/v2024/lib"
)

func main() {
	plugins := lib.DefaultPluginsList()
	plugins = append(plugins, &thumper.Plugin{})

	rr, err := lib.NewRR(".rr.yaml", nil, plugins)
	if err != nil {
		panic(err)
	}

	err = rr.Serve()
	if err != nil {
		panic(err)
	}
}
