package main

import (
	"fmt"
	"os"

	"github.com/jtprogru/loans/storage"
)

func main() {
	storageType := os.Getenv("STORAGE_TYPE")

	store, err := storage.NewStorage(storageType)
	if err != nil {
		fmt.Println("Error creating storage:", err)
		os.Exit(1)
	}

	_ = store
}
