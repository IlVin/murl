package main

import (
	"log"
	"os"
)

func main() {
	// Эти вызовы НЕ должны вызывать предупреждения, так как они в main.main
	panic("allowed in main.main")     // OK - не будет предупреждения? По заданию только запрет на использование panic, даже в main
	log.Fatal("allowed in main.main") // OK - разрешено
	os.Exit(0)                        // OK - разрешено
}

func helper() {
	// А здесь должны быть предупреждения
	panic("should warn")     // want "avoid using panic in production code"
	log.Fatal("should warn") // want "log.Fatal should only be used in main.main"
	os.Exit(1)               // want "os.Exit should only be used in main.main"
}
