package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"path"

	"code.google.com/p/plotinum/plot"
	"code.google.com/p/plotinum/plotter"
	"code.google.com/p/plotinum/plotutil"
	"github.com/mulander/gosnitch"
)

func main() {

	jsonConfig, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}

	var config = gosnitch.Config{}
	err = json.Unmarshal(jsonConfig, &config)
	if err != nil {
		log.Fatal(err)
	}

	project := &gosnitch.Project{
		Command:    exec.Command(config.Command, config.Arguments...),
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

	samplers := make(chan []gosnitch.Data)

	project.Exec(samplers)

	log.Printf("Waiting for the samplers")
	for s := range samplers {
		log.Printf("%+v", s)

		for _, sample := range s {

			p, err := plot.New()
			if err != nil {
				log.Fatal(err)
			}

			p.Title.Text = fmt.Sprintf("%s graph", sample.Label)
			p.X.Label.Text = fmt.Sprintf("tick per %s", project.Sampling)
			p.Y.Label.Text = sample.Label

			pts := make(plotter.XYs, len(sample.Data))
			for i := range pts {
				pts[i].X = float64(i)
				pts[i].Y = sample.Data[i]
			}

			plotutil.AddLinePoints(p, sample.Label, pts)
			plotFile := fmt.Sprintf("%s-%s.png", path.Base(project.Command.Args[0]), sample.Label)

			if err := p.Save(4, 4, plotFile); err != nil {
				log.Fatal(err)
			}
		}
	}
}
