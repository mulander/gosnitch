// Copyright (c) 2012, mulander <netprobe@gmail.com>
// All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.
package main

import (
	"code.google.com/p/plotinum/plot"
	"code.google.com/p/plotinum/plotter"
	"code.google.com/p/plotinum/plotutil"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Sampler interface {
	Sample(cmd *exec.Cmd, ticker *time.Ticker)
	GetData() []Data
	Stop()
}

type Data struct {
	label string
	data  []float64
}

type TopSampler struct {
	Samples []Data
	stop    chan bool
}

func (t *TopSampler) GetData() []Data {
	return t.Samples
}

func (t *TopSampler) Stop() {
	t.stop <- true
}

func (t *TopSampler) Sample(cmd *exec.Cmd, ticker *time.Ticker) {
	// %CPU(field=8) + %MEM(field=9)
	t.stop = make(chan bool)
	t.Samples = make([]Data, 2)
	t.Samples[0].label = "CPU"
	t.Samples[0].data = make([]float64, 1)
	t.Samples[1].label = "MEM"
	t.Samples[1].data = make([]float64, 1)
	raw := "(?m)%d.*$"
	r := regexp.MustCompile(fmt.Sprintf(raw, cmd.Process.Pid))
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			top := exec.Command("top", "-b", "-n 1", fmt.Sprintf("-p %d", cmd.Process.Pid))
			log.Printf("Sampling the process")
			out, err := top.Output()
			if err != nil {
				log.Fatal(err)
			}
			fields := strings.Fields(r.FindString(fmt.Sprintf("%s", out)))
			if len(fields) != 0 {
				cpu, err := strconv.ParseFloat(fields[8], 64)
				if err != nil {
					log.Fatal(err)
				}
				mem, err := strconv.ParseFloat(fields[9], 64)
				if err != nil {
					log.Fatal(err)
				}
				t.Samples[0].data = append(t.Samples[0].data, cpu) // CPU
				t.Samples[1].data = append(t.Samples[1].data, mem) // MEM
				log.Printf("%+v", fields)
			}
		}
	}
}

type Project struct {
	Command    *exec.Cmd     // executable to run during the test
	Duration   time.Duration // duration of a single sample
	Sampling   time.Duration // trigger sampling based on this interval
	Executions int           // the total number of runs for a single test
	Sampler    Sampler
}

func (p *Project) Exec(samplers chan []Data) {
	err := p.Command.Start()
	if err != nil {
		log.Fatal(err)
	}

	ticker := time.NewTicker(p.Sampling)

	// Possibly more samplers in the future
	var wg sync.WaitGroup

	wg.Add(1)
	go func(n int) {
		defer wg.Done()
		p.Sampler.Sample(p.Command, ticker)
		samplers <- p.Sampler.GetData()
	}(1)
	go func() {
		wg.Wait()
		close(samplers)
	}()

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
	log.Printf("Waiting for the ticker to stop")
	ticker.Stop()
	log.Printf("Stopping samplers")
	p.Sampler.Stop()
}

func main() {
	project := &Project{
		Command:    exec.Command("yes"),
		Duration:   10 * time.Second,
		Sampling:   1 * time.Second,
		Executions: 1,
		Sampler:    &TopSampler{}}

	samplers := make(chan []Data)

	project.Exec(samplers)

	log.Printf("Waiting for the samplers")
	for s := range samplers {
		log.Printf("%+v", s)

		for _, sample := range s {

			p, err := plot.New()
			if err != nil {
				log.Fatal(err)
			}

			p.Title.Text = fmt.Sprintf("%s graph", sample.label)
			p.X.Label.Text = fmt.Sprintf("tick per %s", project.Sampling)
			p.Y.Label.Text = sample.label

			pts := make(plotter.XYs, len(sample.data))
			for i := range pts {
				pts[i].X = float64(i)
				pts[i].Y = sample.data[i]
			}

			plotutil.AddLinePoints(p, sample.label, pts)
			plotFile := fmt.Sprintf("%s-%s.png", project.Command.Args[0], sample.label)

			if err := p.Save(4, 4, plotFile); err != nil {
				log.Fatal(err)
			}
		}
	}
}
