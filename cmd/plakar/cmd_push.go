package main

import (
	"log"
	"os"

	"github.com/poolpOrg/plakar/repository"
)

func cmd_push(pstore repository.Store, args []string) {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	snapshot := pstore.Transaction().Snapshot()
	if len(args) == 0 {
		snapshot.Push(dir)
	} else {
		for i := 0; i < len(args); i++ {
			snapshot.Push(args[i])
		}
	}
	snapshot.Commit()
}
