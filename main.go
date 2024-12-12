package main

import (
	"fmt"
	"log"
	"os"
	"io/ioutil"
)

func main() {
	args := os.Args
	filename := ".DS_Store"
	if len(args) == 2 {
		filename = args[1]
	} else if len(args) > 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <.DS_Store file>\n", args[0])
		os.Exit(1)
	} else {
		fmt.Fprintf(os.Stderr, "File unspecified. Using .DS_Store in the current directory...\n")
	}

	content, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}

	ds := NewDSStore(content)
	if err := ds.Parse(); err != nil {
		log.Fatal(err)
	}

	for _, record := range ds.readRecords() {
		fmt.Println(record.name)
		for _, line := range record.humanReadable() {
			fmt.Printf("\t%s\n", line)
		}
	}
}
