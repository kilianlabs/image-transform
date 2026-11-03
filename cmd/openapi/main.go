package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/kilianlabs/image-transform/internal/httpapi"
)

func main() {
	check := flag.String("check", "", "check a saved contract for drift")
	write := flag.String("write", "", "write the generated contract")
	flag.Parse()
	data, err := httpapi.New(nil, nil).OpenAPI()
	if err == nil {
		switch {
		case *check != "":
			var saved []byte
			saved, err = os.ReadFile(*check)
			if err == nil && !bytes.Equal(data, saved) {
				err = fmt.Errorf("OpenAPI contract is out of date; run make openapi")
			}
		case *write != "":
			err = os.WriteFile(*write, data, 0644)
		default:
			_, err = os.Stdout.Write(data)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
