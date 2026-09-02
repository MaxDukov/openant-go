package main

import (
	"fmt"

	"github.com/maxdukov/openant-go/ant"
)

// runSticks prints all attached ANT sticks so the user can pick one for
// `goant scan -serial`, or several for -serials/-all (openant issues
// #67/#91).
func runSticks() {
	sticks := ant.Sticks()
	if len(sticks) == 0 {
		fmt.Println("No ANT sticks found.")
		return
	}
	fmt.Println("Attached ANT sticks:")
	serialless := false
	for _, s := range sticks {
		fmt.Printf("  %s\n", s)
		if s.Serial == "" {
			serialless = true
		}
	}
	fmt.Println("\nUse with:")
	fmt.Println("  goant scan -serial <number>   single stick with a readable serial")
	fmt.Println("  goant scan -serials a,b       several sticks (serials or bus:addr)")
	fmt.Println("  goant scan -all               every attached stick")
	if serialless {
		fmt.Println("\nNote: some sticks (e.g. CYCPLUS clones) report broken USB serial")
		fmt.Println("descriptors; select those by bus:addr, e.g. -serials 1:3.")
	}
}
