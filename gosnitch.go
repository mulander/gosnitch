package main

import (
	"log"
	"os/exec"
	"time"
)

func main() {
	cmd := exec.Command("yes")
	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	done := make(chan error)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case <-time.After(10 * time.Second):
		if err := cmd.Process.Kill(); err != nil {
			log.Fatal("Failed to kill: ", err)
		}
		<-done
		log.Println("Process killed")
	case err := <-done:
		log.Printf("Process done with error = %v", err)
	}
}
