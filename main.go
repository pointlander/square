package main

import (
	"flag"
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"sync"

	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/gonum/stat"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"

	"github.com/pointlander/datum/iris"
	"github.com/pointlander/gradient/tf32"
)

type Mode int

const (
	ModeNone Mode = iota
	ModeOrthogonality
	ModeEntropy
)

const (
	Width     = 16
	Width2    = 16
	Width3    = 16
	BatchSize = 60
	Eta       = .6
)

var (
	datum iris.Datum
	once  sync.Once
)

func load() {
	var err error
	datum, err = iris.Load()
	if err != nil {
		panic(err)
	}
	max := make([]float64, 4)
	for _, item := range datum.Fisher {
		for i, measure := range item.Measures {
			if measure > max[i] {
				max[i] = measure
			}
		}
	}
	for _, item := range datum.Fisher {
		for i, measure := range item.Measures {
			item.Measures[i] = measure / max[i]
		}
	}

	max = make([]float64, 4)
	for _, item := range datum.Bezdek {
		for i, measure := range item.Measures {
			if measure > max[i] {
				max[i] = measure
			}
		}
	}
	for _, item := range datum.Bezdek {
		for i, measure := range item.Measures {
			item.Measures[i] = measure / max[i]
		}
	}
}

var colors = [...]color.RGBA{
	{R: 0xff, G: 0x00, B: 0x00, A: 255},
	{R: 0x00, G: 0xff, B: 0x00, A: 255},
	{R: 0x00, G: 0x00, B: 0xff, A: 255},
}

func plotData(data *mat.Dense, name string, training []iris.Iris) {
	rows, cols := data.Dims()

	var pc stat.PC
	ok := pc.PrincipalComponents(data, nil)
	if !ok {
		return
	}

	k := 2
	var projection mat.Dense
	projection.Mul(data, pc.VectorsTo(nil).Slice(0, cols, 0, k))

	p, err := plot.New()
	if err != nil {
		panic(err)
	}

	p.Title.Text = "iris"
	p.X.Label.Text = "x"
	p.Y.Label.Text = "y"
	p.Legend.Top = true

	for i := 0; i < 3; i++ {
		label := ""
		points := make(plotter.XYs, 0, rows)
		for j := 0; j < rows; j++ {
			if iris.Labels[training[j].Label] != i {
				continue
			}
			label = training[j].Label
			points = append(points, plotter.XY{X: projection.At(j, 0), Y: projection.At(j, 1)})
		}

		scatter, err := plotter.NewScatter(points)
		if err != nil {
			panic(err)
		}
		scatter.GlyphStyle.Radius = vg.Length(1)
		scatter.GlyphStyle.Shape = draw.CircleGlyph{}
		scatter.GlyphStyle.Color = colors[i]
		p.Add(scatter)
		p.Legend.Add(fmt.Sprintf("%s", label), scatter)
	}

	err = p.Save(8*vg.Inch, 8*vg.Inch, name)
	if err != nil {
		panic(err)
	}

	missed := 0
	for i := 0; i < rows; i++ {
		max, match := -1.0, 0
		for j := 0; j < rows; j++ {
			if j == i {
				continue
			}
			sumAB, sumAA, sumBB := 0.0, 0.0, 0.0
			for k := 0; k < cols; k++ {
				a, b := data.At(i, k), data.At(j, k)
				sumAB += a * b
				sumAA += a * a
				sumBB += b * b
			}
			similarity := sumAB / (math.Sqrt(sumAA) * math.Sqrt(sumBB))
			if similarity > max {
				max, match = similarity, j
			}
		}
		should := iris.Labels[training[i].Label]
		found := iris.Labels[training[match].Label]
		if should != found {
			fmt.Println(max)
			missed++
		}
	}
	fmt.Println("missed", missed)
}

func neuralNetwork(name string, training []iris.Iris, mode Mode) {
	fmt.Println(name)
	rnd := rand.New(rand.NewSource(1))
	random32 := func(a, b float32) float32 {
		return (b-a)*rnd.Float32() + a
	}
	input, output := tf32.NewV(4, BatchSize), tf32.NewV(4, BatchSize)
	w1, b1, w2, b2 := tf32.NewV(4, Width), tf32.NewV(Width), tf32.NewV(Width, Width2), tf32.NewV(Width2)
	w3, b3, w4, b4 := tf32.NewV(Width2, Width3), tf32.NewV(Width3), tf32.NewV(Width3, 4), tf32.NewV(4)
	parameters := []*tf32.V{&w1, &b1, &w2, &b2, &w3, &b3, &w4, &b4}
	for _, p := range parameters {
		for i := 0; i < cap(p.X); i++ {
			p.X = append(p.X, random32(-1, 1))
		}
	}
	ones := tf32.NewV(BatchSize)
	for i := 0; i < cap(ones.X); i++ {
		ones.X = append(ones.X, 1)
	}
	l1 := tf32.Sigmoid(tf32.Add(tf32.Mul(w1.Meta(), input.Meta()), b1.Meta()))
	l2 := tf32.Sigmoid(tf32.Add(tf32.Mul(w2.Meta(), l1), b2.Meta()))
	l3 := tf32.Sigmoid(tf32.Add(tf32.Mul(w3.Meta(), l2), b3.Meta()))
	l4 := tf32.Sigmoid(tf32.Add(tf32.Mul(w4.Meta(), l3), b4.Meta()))
	cost := tf32.Avg(tf32.Sub(ones.Meta(), tf32.Similarity(l4, output.Meta())))
	//cost := tf32.Avg(tf32.Quadratic(l4, output.Meta()))

	length := len(training)
	learn := func(mode Mode) {
		data := make([]*iris.Iris, 0, length)
		for i := range training {
			data = append(data, &training[i])
		}

		iterations := 1000
		switch mode {
		case ModeNone:
		case ModeOrthogonality:
			iterations = 1000
		case ModeEntropy:
			iterations = 1000
		}
		for i := 0; i < iterations; i++ {
			for i := range data {
				j := i + rnd.Intn(length-i)
				data[i], data[j] = data[j], data[i]
			}
			total := float32(0.0)
			for j := 0; j < length; j += BatchSize {
				for _, p := range parameters {
					p.Zero()
				}
				input.Zero()
				output.Zero()
				ones.Zero()

				values := make([]float32, 0, 4*BatchSize)
				for k := 0; k < BatchSize; k++ {
					index := (j + k) % length
					for _, measure := range data[index].Measures {
						values = append(values, float32(measure))
					}
				}
				input.Set(values)
				output.Set(values)
				total += tf32.Gradient(cost).X[0]

				norm := float32(0)
				for k, p := range parameters {
					for l, d := range p.D {
						if math.IsNaN(float64(d)) {
							fmt.Println(d, k, l)
							return
						} else if math.IsInf(float64(d), 0) {
							fmt.Println(d, k, l)
							return
						}
						norm += d * d
					}
				}
				norm = float32(math.Sqrt(float64(norm)))
				if norm > 1 {
					scaling := 1 / norm
					for _, p := range parameters {
						for l, d := range p.D {
							p.X[l] -= Eta * d * scaling
						}
					}
				} else {
					for _, p := range parameters {
						for l, d := range p.D {
							p.X[l] -= Eta * d
						}
					}
				}
			}
			fmt.Println(total)
		}
	}

	learn(ModeNone)
	switch mode {
	case ModeNone:
		//learn(mode)
	case ModeOrthogonality:
		fmt.Println("orthogonality learning...")
		weight := tf32.NewV(1)
		weight.X = append(weight.X, 1)
		cost = tf32.Add(cost, tf32.Hadamard(weight.Meta(), tf32.Avg(tf32.Abs(tf32.Orthogonality(l2)))))
		learn(mode)
	case ModeEntropy:
		fmt.Println("entropy learning...")
		weight := tf32.NewV(1)
		weight.X = append(weight.X, 1)
		cost = tf32.Add(cost, tf32.Hadamard(weight.Meta(), tf32.Avg(tf32.Entropy(tf32.Softmax(tf32.T(l2))))))
		learn(mode)
	}

	input = tf32.NewV(4)
	l1 = tf32.Sigmoid(tf32.Add(tf32.Mul(w1.Meta(), input.Meta()), b1.Meta()))
	l2 = tf32.Sigmoid(tf32.Add(tf32.Mul(w2.Meta(), l1), b2.Meta()))
	tf32.Static.InferenceOnly = true
	defer func() {
		tf32.Static.InferenceOnly = false
	}()
	points := make([]float64, 0, Width2*length)
	for i := range training {
		values := make([]float32, 0, 4)
		for _, measure := range training[i].Measures {
			values = append(values, float32(measure))
		}
		input.Set(values)
		l2(func(a *tf32.V) {
			for _, value := range a.X {
				points = append(points, float64(value))
			}
		})
	}
	plotData(mat.NewDense(length, Width, points), name, training)
}

var (
	orthogonality = flag.Bool("orthogonality", false, "orthogonality mode")
	entropy       = flag.Bool("entropy", false, "entropy mode")
)

func main() {
	flag.Parse()

	once.Do(load)
	training := datum.Fisher
	length := len(training)
	data := make([]float64, 0, length*4)
	for _, item := range training {
		for _, measure := range item.Measures {
			data = append(data, measure)
		}
	}
	plotData(mat.NewDense(length, 4, data), "iris.png", training)

	neuralNetwork("embedding.png", training, ModeNone)
	if *orthogonality {
		neuralNetwork("orthogonality_embedding.png", training, ModeOrthogonality)
	}
	if *entropy {
		neuralNetwork("entropy_embedding.png", training, ModeEntropy)
	}
}
