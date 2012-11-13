package main

import (
	"log"
	"os/exec"
	"time"
)

type Project struct {
	Command    *exec.Cmd     // executable to run during the test
	Duration   time.Duration // duration of a single sample
	Sampling   time.Duration // trigger sampling based on this interval
	Executions int           // the total number of runs for a single test
}

func (p *Project) Exec() {
	err := p.Command.Start()
	if err != nil {
		log.Fatal(err)
	}

	ticker := time.NewTicker(p.Sampling)
	go p.Sample(ticker)

	done := make(chan error)
	go func() {
		done <- p.Command.Wait()
	}()
	select {
	case <-time.After(p.Duration):
		if err := p.Command.Process.Kill(); err != nil {
			log.Fatal("Failed to kill: ", err)
		}
		<-done
		log.Println("Process killed")
	case err := <-done:
		log.Printf("Process done with error = %v", err)
	}
	ticker.Stop()
}

func (p *Project) Sample(ticker *time.Ticker) {
	for {
		<-ticker.C
		log.Printf("Sampling the process")
	}
}

func main() {
	project := &Project{
		Command:    exec.Command("yes"),
		Duration:   10 * time.Second,
		Sampling:   1 * time.Second,
		Executions: 1}

	project.Exec()
}
