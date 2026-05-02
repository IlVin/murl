package a

import (
	"log"
	"os"
)

func good() {
	// Нет panic, log.Fatal, os.Exit - всё ок
}

func badPanic() {
	panic("should trigger warning") // want "avoid using panic in production code"
}

func badLogFatal() {
	log.Fatal("should trigger warning") // want "log.Fatal should only be used in main.main"
}

func badLogFatalf() {
	log.Fatalf("should trigger warning: %s", "test") // want "log.Fatalf should only be used in main.main"
}

func badLogFatalln() {
	log.Fatalln("should trigger warning") // want "log.Fatalln should only be used in main.main"
}

func badOsExit() {
	os.Exit(1) // want "os.Exit should only be used in main.main"
}
