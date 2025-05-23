package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	fmt.Println("Running all tests for prerender-url-shortener...")

	// Run tests with coverage
	cmd := exec.Command("go", "test", "-v", "-cover", "./...")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		fmt.Printf("Tests failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nAll tests passed!")

	// Run benchmarks
	fmt.Println("\nRunning benchmarks...")
	benchCmd := exec.Command("go", "test", "-bench=.", "-benchmem", "./...")
	benchCmd.Stdout = os.Stdout
	benchCmd.Stderr = os.Stderr

	err = benchCmd.Run()
	if err != nil {
		fmt.Printf("Benchmarks failed: %v\n", err)
		// Don't exit on benchmark failure
	}

	fmt.Println("\nTest run complete!")
}
