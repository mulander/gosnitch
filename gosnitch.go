// Copyright (c) 2012, mulander <netprobe@gmail.com>
// All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.
package gosnitch

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ByteSize float64

const (
	_           = iota // ignore first value by assigning to blank identifier
	KB ByteSize = 1 << (10 * iota)
	MB
	GB
	TB
	PB
	EB
	ZB
	YB
)

type Sampler interface {
	Probe(pid int)
	Sample(pid int, ticker *time.Ticker)
	GetData() []Data
	Stop()
}

type Data struct {
	Label string
	Data  []float64
}

type TopSampler struct {
	Samples []Data
	stop    chan bool
	regex   *regexp.Regexp
}

func (t *TopSampler) GetData() []Data {
	return t.Samples
}

func (t *TopSampler) Stop() {
	t.stop <- true
}

func (t *TopSampler) toMB(field string) float64 {
	strlen := len(field) - 1
	value, err := strconv.ParseFloat(field[:strlen], 64)
	if err != nil {
		log.Fatal(err)
	}

	unit := ByteSize(value)

	switch field[strlen:] {
	case "m": // do nothing, correct unit
	case "g": // convert to MB
		unit = (unit * GB) / MB
	default: // convert to MB
		value, err := strconv.ParseFloat(field, 64)
		if err != nil {
			log.Fatal(err)
		}
		unit = ByteSize(value)
		unit = (unit * KB) / MB
	}
	return float64(unit)
}

func NewTopSampler(pid int) Sampler {
	sampler := &TopSampler{}
	sampler.stop = make(chan bool)
	sampler.Samples = make([]Data, 5)
	// %CPU(field=8) + %MEM(field=9)
	sampler.Samples[0].Label = "CPU"
	sampler.Samples[0].Data = make([]float64, 0)
	sampler.Samples[1].Label = "MEM"
	sampler.Samples[1].Data = make([]float64, 0)
	sampler.Samples[2].Label = "VIRT (m)" // top field 4
	sampler.Samples[2].Data = make([]float64, 0)
	sampler.Samples[3].Label = "RES (m)" // top field 5
	sampler.Samples[3].Data = make([]float64, 0)
	sampler.Samples[4].Label = "SHR (m)" // top field 6
	sampler.Samples[4].Data = make([]float64, 0)
	raw := "(?m)%d.*$"
	sampler.regex = regexp.MustCompile(fmt.Sprintf(raw, pid))
	return sampler
}

// Take a single sample of the process
func (t *TopSampler) Probe(pid int) {
	top := exec.Command("top", "-b", "-n 1", fmt.Sprintf("-p %d", pid))
	log.Printf("Sampling the process")
	out, err := top.Output()
	if err != nil {
		log.Fatal(err)
	}
	fields := strings.Fields(t.regex.FindString(fmt.Sprintf("%s", out)))
	if len(fields) != 0 {
		cpu, err := strconv.ParseFloat(fields[8], 64)
		if err != nil {
			log.Fatal(err)
		}
		mem, err := strconv.ParseFloat(fields[9], 64)
		if err != nil {
			log.Fatal(err)
		}
		virt := t.toMB(fields[4])
		res := t.toMB(fields[5])
		shr := t.toMB(fields[6])

		t.Samples[0].Data = append(t.Samples[0].Data, cpu) // CPU
		t.Samples[1].Data = append(t.Samples[1].Data, mem) // MEM
		t.Samples[2].Data = append(t.Samples[2].Data, virt)
		t.Samples[3].Data = append(t.Samples[3].Data, res)
		t.Samples[4].Data = append(t.Samples[4].Data, shr)
		log.Printf("%+v", fields)
	}
}

// Take a sample based on a time.Ticker interval
func (t *TopSampler) Sample(pid int, ticker *time.Ticker) {
	t.stop = make(chan bool)
	raw := "(?m)%d.*$"
	t.regex = regexp.MustCompile(fmt.Sprintf(raw, pid))
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			t.Probe(pid)
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
		p.Sampler.Sample(p.Command.Process.Pid, ticker)
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
	Arguments  []string
	Directory  string
	Duration   string
	Sampling   string
	Executions int
	Sampler    string
}

func (c *Config) GetDuration() time.Duration {
	dur, err := time.ParseDuration(c.Duration)
	if err != nil {
		log.Fatal(err)
	}
	return dur
}

func (c *Config) GetSampling() time.Duration {
	dur, err := time.ParseDuration(c.Duration)
	if err != nil {
		log.Fatal(err)
	}
	return dur
}

func (c *Config) GetSampler() Sampler {
	if c.Sampler != "TopSampler" {
		log.Fatal("Unknown sampler")
	}
	return NewTopSampler(0)
}

func Pidof(name string) int {
	cmd := exec.Command("pidof", name)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Start(); err != nil {
		log.Fatalf("Pidof fail: %s", err)
	}

	if err := cmd.Wait(); err != nil {
		log.Fatalf("Pidof.Wait fail: %s", err)
	}

	pid, err := strconv.ParseInt(strings.Trim(out.String(), "\n"), 10, 32)
	if err != nil {
		log.Fatal(err)
	}
	return int(pid)
}
