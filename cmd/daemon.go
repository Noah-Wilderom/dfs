package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("Daemon started...")
	for {
		time.Sleep(5 * time.Second)
	}
}
