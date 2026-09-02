package main

import (
	"fmt"

	"github.com/maxdukov/openant-go/ant"
)

// runSticks prints the serial numbers of all attached ANT sticks so the
// user can pick one for `goant scan -serial` (openant issue #116).
func runSticks() {
	serials := ant.Serials()
	if len(serials) == 0 {
		fmt.Println("No ANT sticks found.")
		return
	}
	fmt.Println("Attached ANT sticks:")
	for _, s := range serials {
		fmt.Printf("  %s\n", s)
	}
	fmt.Println("\nUse with: goant scan -serial <number>")
}
