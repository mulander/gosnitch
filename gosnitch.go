// Copyright (c) 2012, mulander <netprobe@gmail.com>
// All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.
package main

import (
	"code.google.com/p/plotinum/plot"
	"code.google.com/p/plotinum/plotter"
	"code.google.com/p/plotinum/plotutil"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"path"
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
	t.Samples = make([]Data, 5)
	t.Samples[0].label = "CPU"
	t.Samples[0].data = make([]float64, 1)
	t.Samples[1].label = "MEM"
	t.Samples[1].data = make([]float64, 1)
	t.Samples[2].label = "VIRT (m)" // top field 4
	t.Samples[2].data = make([]float64, 1)
	t.Samples[3].label = "RES (m)" // top field 5
	t.Samples[3].data = make([]float64, 1)
	t.Samples[4].label = "SHR (m)" // top field 6
	t.Samples[4].data = make([]float64, 1)
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
				virt, err := strconv.ParseFloat(strings.Replace(fields[4], "m", "", 1), 64)
				if err != nil {
					log.Fatal(err)
				}
				res, err := strconv.ParseFloat(strings.Replace(fields[5], "m", "", 1), 64)
				if err != nil {
					log.Fatal(err)
				}
				shr, err := strconv.ParseFloat(strings.Replace(fields[6], "m", "", 1), 64)
				if err != nil {
					log.Fatal(err)
				}
				t.Samples[0].data = append(t.Samples[0].data, cpu) // CPU
				t.Samples[1].data = append(t.Samples[1].data, mem) // MEM
				t.Samples[2].data = append(t.Samples[2].data, virt)
				t.Samples[3].data = append(t.Samples[3].data, res)
				t.Samples[4].data = append(t.Samples[3].data, shr)
				log.Printf("%+v", fields)
			}
		}
	}
}

type Project struct {
	Command    *exec.Cmd     // executable to run during the test
	Directory  string        // working directory for running the project
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

type Config struct {
	Command    string
	Directory  string
	Duration   string
	Sampling   string
	Executions int
	Sampler    string
}

func (c *Config) toDuration(field string) time.Duration {
	strlen := len(field) - 1
	value, err := strconv.ParseFloat(field[:strlen], 64)
	if err != nil {
		log.Fatal(err)
	}

	duration := time.Duration(value)

	switch field[strlen:] {
	case "h":
		duration *= time.Hour
	case "m":
		duration *= time.Minute
	case "s":
		duration *= time.Second
	}
	return duration
}

func (c *Config) GetDuration() time.Duration {
	return c.toDuration(c.Duration)
}

func (c *Config) GetSampling() time.Duration {
	return c.toDuration(c.Sampling)
}

func (c *Config) GetSampler() Sampler {
	if c.Sampler != "TopSampler" {
		log.Fatal("Unknown sampler")
	}
	return &TopSampler{}
}

func main() {

	jsonConfig, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}

	var config = Config{}
	err = json.Unmarshal(jsonConfig, &config)
	if err != nil {
		log.Fatal(err)
	}

	project := &Project{
		Command:    exec.Command(config.Command),
		Directory:  config.Directory,
		Duration:   config.GetDuration(),
		Sampling:   config.GetSampling(),
		Executions: config.Executions,
		Sampler:    config.GetSampler()}

	// Change the working directory if needed
	if project.Directory != "" {
		project.Command.Dir = project.Directory
		log.Printf("Set project directory to: %s", project.Directory)
	}

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
			plotFile := fmt.Sprintf("%s-%s.png", path.Base(project.Command.Args[0]), sample.label)

			if err := p.Save(4, 4, plotFile); err != nil {
				log.Fatal(err)
			}
		}
	}
}
