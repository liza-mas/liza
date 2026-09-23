package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"sort"
)

// cmdWeights refreshes the weights file from downloaded Windows CI artifacts:
// each ops top-level test and each package gets the median of its observed
// elapsed seconds; entries without new observations are kept.
func cmdWeights(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("weights", flag.ContinueOnError)
	base := fs.String("base", "", "existing weights JSON file")
	source := fs.String("source", "", "description of the new observations (stored in the file)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *base == "" || fs.NArg() == 0 {
		return errors.New("usage: testshard weights -base W PATH [PATH ...]")
	}
	w, err := loadWeights(*base)
	if err != nil {
		return err
	}
	files, err := collectJSON(fs.Args())
	if err != nil {
		return err
	}
	tests, pkgs := map[string][]float64{}, map[string][]float64{}
	for _, f := range files {
		p, t, err := readResultsFile(f[0], f[1])
		if err != nil {
			return err
		}
		for _, r := range t {
			if r.Package == w.OpsPackage && r.Action != "skip" {
				tests[r.Test] = append(tests[r.Test], r.Elapsed)
			}
		}
		for _, r := range p {
			if r.Package != w.OpsPackage {
				pkgs[r.Package] = append(pkgs[r.Package], r.Elapsed)
			}
		}
	}
	if w.Tests == nil {
		w.Tests = map[string]float64{}
	}
	if w.Packages == nil {
		w.Packages = map[string]float64{}
	}
	for k, v := range tests {
		w.Tests[k] = roundTo(median(v), 3)
	}
	for k, v := range pkgs {
		w.Packages[k] = roundTo(median(v), 1)
	}
	if *source != "" {
		w.Source = *source
	}
	_, err = fmt.Fprint(out, mustJSON(w))
	return err
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func roundTo(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}
