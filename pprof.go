//go:build pprof

package main

import (
	"net/http"
	_ "net/http/pprof"
)

func init() {
	go func() {
		http.ListenAndServe("localhost:6969", nil)
	}()
}
