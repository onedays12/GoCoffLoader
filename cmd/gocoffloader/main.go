package main

import (
	"fmt"
	"log"
	"os"

	"github.com/onedays12/GoCoffLoader/pkg/coff"
)

func main() {
	bofPath := "whoami.x64.o"

	// 如果命令行提供了参数，则使用第一个参数作为路径
	if len(os.Args) > 1 {
		bofPath = os.Args[1]
	}

	fmt.Printf("[*] Reading BOF file: %s\n", bofPath)

	coffBytes, err := os.ReadFile(bofPath)
	if err != nil {
		log.Fatalf("[!] Failed to read file: %v", err)
	}

	fmt.Println("[*] Executing BOF...")

	result, err := coff.LoadWithMethod(coffBytes, []byte{}, "go")
	if err != nil {
		log.Fatalf("[!] Load failed: %v", err)
	}

	fmt.Println("--- BOF Output ---")
	fmt.Print(result)
}
