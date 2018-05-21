package util

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
)

func CopyFile(source, destination string) {
	sfile, err := os.Stat(source)
	if err != nil {
		log.Fatal(err)
	}
	fmode := sfile.Mode()
	from, err := os.Open(source)
	if err != nil {
		log.Fatal(err)
	}
	defer from.Close()

	to, err := os.OpenFile(destination, os.O_RDWR|os.O_CREATE, fmode) // 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer to.Close()

	_, err = io.Copy(to, from)
	if err != nil {
		log.Fatal(err)
	}
}

func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return true
}

func AppendStrings(lines []string, filename string, termination string) error {
	file, err := os.Open(filename)
	if err != nil && os.IsNotExist(err) {
		if file, err = os.Create(filename); err != nil {
			return err
		}
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	for _, line := range lines {
		fmt.Fprintln(w, line+termination)
	}
	return w.Flush()
}
