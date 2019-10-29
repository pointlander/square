package main

import (
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/pointlander/datum/iris"
)

// Embeddings is a set of embeddings
type Embeddings struct {
	Columns    int
	Network    *Network
	Embeddings []Embedding
}

// Embedding is an embedding with a label and features
type Embedding struct {
	iris.Iris
	Source   int
	Features []float64
}

// Copy makes a copy of the embeddings
func (e *Embeddings) Copy() Embeddings {
	embeddings := Embeddings{
		Columns:    e.Columns,
		Embeddings: make([]Embedding, len(e.Embeddings)),
	}
	copy(embeddings.Embeddings, e.Embeddings)
	return embeddings
}

// Variance computes the variance for the features with column
func (e *Embeddings) Variance(column int) float64 {
	n, sum := float64(len(e.Embeddings)), 0.0
	for _, row := range e.Embeddings {
		sum += row.Features[column]
	}
	average, variance := sum/n, 0.0
	for _, row := range e.Embeddings {
		v := row.Features[column] - average
		variance += v * v
	}
	return variance / n
}

// PivotVariance computes the variance for the left and right features with column
func (e *Embeddings) PivotVariance(column int, pivot float64) (left, right float64) {
	nLeft, nRight, sumLeft, sumRight := 0, 0, 0.0, 0.0
	for _, row := range e.Embeddings {
		if value := row.Features[column]; value > pivot {
			nRight++
			sumRight += value
		} else {
			nLeft++
			sumLeft += value
		}
	}
	averageLeft, averageRight := sumLeft, sumRight
	if nLeft != 0 {
		averageLeft /= float64(nLeft)
	}
	if nRight != 0 {
		averageRight /= float64(nRight)
	}
	for _, row := range e.Embeddings {
		if value := row.Features[column]; value > pivot {
			v := value - averageRight
			right += v * v
		} else {
			v := value - averageLeft
			left += v * v
		}
	}
	if nLeft != 0 {
		left /= float64(nLeft)
	}
	if nRight != 0 {
		right /= float64(nRight)
	}
	return left, right
}

// VarianceReduction implements variance reduction algorithm
func (e *Embeddings) VarianceReduction(depth int, label, count uint) *Reduction {
	length := len(e.Embeddings)
	if length == 0 {
		return nil
	}

	reduction := Reduction{
		Embeddings: e,
		Label:      label,
	}
	if depth <= 0 {
		return &reduction
	}

	for k := 0; k < e.Columns; k++ {
		total := e.Variance(k)
		for _, row := range e.Embeddings {
			pivot := row.Features[k]
			a, b := e.PivotVariance(k, pivot)
			if cost := total - (a + b); cost > reduction.Max {
				reduction.Max, reduction.Column, reduction.Pivot = cost, k, pivot
			}
		}
	}

	left := Embeddings{
		Columns:    e.Columns,
		Network:    e.Network,
		Embeddings: make([]Embedding, 0, length),
	}
	right := Embeddings{
		Columns:    e.Columns,
		Network:    e.Network,
		Embeddings: make([]Embedding, 0, length),
	}
	for _, row := range e.Embeddings {
		if row.Features[reduction.Column] > reduction.Pivot {
			right.Embeddings = append(right.Embeddings, row)
		} else {
			left.Embeddings = append(left.Embeddings, row)
		}
	}
	reduction.Left, reduction.Right =
		left.VarianceReduction(depth-1, label, count+1),
		right.VarianceReduction(depth-1, label|(1<<count), count+1)
	return &reduction
}

// PrintTable prints a table of embeddings
func (r *Reduction) PrintTable(out *os.File, mode Mode, cutoff float64) {
	if out == nil {
		return
	}

	fmt.Fprintf(out, "# Training cost vs epochs\n")
	fmt.Fprintf(out, "![epochs of %s](epochs_%s.png?raw=true)]\n\n", mode.String(), mode.String())

	fmt.Fprintf(out, "# Decision tree\n")
	fmt.Fprintf(out, "```go\n")
	fmt.Fprintf(out, "%s\n", r.String())
	fmt.Fprintf(out, "```\n\n")

	headers, rows := make([]string, 0, Width2+2), make([][]string, 0, 256)
	headers = append(headers, "label", "cluster")
	for i := 0; i < r.Embeddings.Columns; i++ {
		headers = append(headers, fmt.Sprintf("%d", i))
	}

	var load func(r *Reduction)
	load = func(r *Reduction) {
		if r == nil {
			return
		}
		if (r.Left == nil && r.Right == nil) || r.Max < cutoff {
			for _, item := range r.Embeddings.Embeddings {
				row := make([]string, 0, r.Embeddings.Columns+2)
				label, predicted := item.Label, r.Label
				row = append(row, label, fmt.Sprintf("%d", predicted))
				for _, value := range item.Features {
					row = append(row, fmt.Sprintf("%f", value))
				}
				rows = append(rows, row)
			}
			return
		}
		load(r.Left)
		load(r.Right)
	}
	load(r.Left)
	load(r.Right)

	fmt.Fprintf(out, "# Output of neural network middle layer\n")
	printTable(out, headers, rows)
	fmt.Fprintf(out, "\n")

	plotData(r.Embeddings, fmt.Sprintf("results/embedding_%s.png", mode.String()))
	fmt.Fprintf(out, "# PCA of network middle layer\n")
	fmt.Fprintf(out, "![embedding of %s](embedding_%s.png?raw=true)]\n", mode.String(), mode.String())
}

// GetMislabeled computes how many embeddings are mislabeled
func (r *Reduction) GetMislabeled(cutoff float64) (mislabeled uint) {
	counts := make(map[string]map[uint]uint)
	var count func(r *Reduction)
	count = func(r *Reduction) {
		if r == nil {
			return
		}
		if (r.Left == nil && r.Right == nil) || r.Max < cutoff {
			for _, item := range r.Embeddings.Embeddings {
				label, predicted := item.Label, r.Label
				count, ok := counts[label]
				if !ok {
					count = make(map[uint]uint)
					counts[label] = count
				}
				count[predicted]++
			}
			return
		}
		count(r.Left)
		count(r.Right)
	}
	count(r.Left)
	count(r.Right)

	type Triple struct {
		Label     string
		Predicted uint
		Count     uint
	}
	triples := make([]Triple, 0, 8)
	for label, count := range counts {
		for predicted, c := range count {
			triples = append(triples, Triple{
				Label:     label,
				Predicted: predicted,
				Count:     c,
			})
		}
	}
	sort.Slice(triples, func(i, j int) bool {
		return triples[i].Count > triples[j].Count
	})
	labels, used := make(map[string]uint), make(map[uint]bool)
	for _, triple := range triples {
		if _, ok := labels[triple.Label]; !ok {
			if !used[triple.Predicted] {
				labels[triple.Label], used[triple.Predicted] = triple.Predicted, true
			}
		}
	}

	var miss func(r *Reduction)
	miss = func(r *Reduction) {
		if r == nil {
			return
		}
		if (r.Left == nil && r.Right == nil) || r.Max < cutoff {
			for _, item := range r.Embeddings.Embeddings {
				label, predicted := item.Label, r.Label
				if l, ok := labels[label]; !ok || l != predicted {
					mislabeled++
				}
			}
			return
		}
		miss(r.Left)
		miss(r.Right)
	}
	miss(r.Left)
	miss(r.Right)

	return mislabeled
}

// GetConsistency returns zero if the data is self consistent
func (r *Reduction) GetConsistency() (consistency uint) {
	embeddings := r.Embeddings
	for i, x := range embeddings.Embeddings {
		max, match := -1.0, 0
		for j, y := range embeddings.Embeddings {
			if j == i {
				continue
			}
			sumAB, sumAA, sumBB := 0.0, 0.0, 0.0
			for k, a := range x.Features {
				b := y.Features[k]
				sumAB += a * b
				sumAA += a * a
				sumBB += b * b
			}
			similarity := sumAB / (math.Sqrt(sumAA) * math.Sqrt(sumBB))
			if similarity > max {
				max, match = similarity, j
			}
		}
		should := iris.Labels[embeddings.Embeddings[i].Label]
		found := iris.Labels[embeddings.Embeddings[match].Label]
		if should != found {
			consistency++
		}
	}
	return consistency
}

// Reduction is the result of variance reduction
type Reduction struct {
	Embeddings  *Embeddings
	Label       uint
	Column      int
	Pivot       float64
	Max         float64
	Left, Right *Reduction
}

// String converts the reduction to a string representation
func (r *Reduction) String() string {
	var serialize func(r *Reduction, label, depth uint) string
	serialize = func(r *Reduction, label, depth uint) string {
		spaces := ""
		for i := uint(0); i < depth; i++ {
			spaces += " "
		}
		left, right := "", ""
		if r.Left != nil && (r.Left.Left != nil || r.Left.Right != nil) {
			left = serialize(r.Left, label, depth+1)
		}
		if r.Right != nil && (r.Right.Left != nil || r.Right.Right != nil) {
			right = serialize(r.Right, label|(1<<depth), depth+1)
		}
		layer := fmt.Sprintf("%s// variance reduction: %f\n", spaces, r.Max)
		layer += fmt.Sprintf("%sif output[%d] > %f {\n", spaces, r.Column, r.Pivot)
		if right == "" {
			layer += fmt.Sprintf("%s label := %d\n", spaces, label|(1<<depth))
		} else {
			layer += fmt.Sprintf("%s\n", right)
		}
		layer += fmt.Sprintf("%s} else {\n", spaces)
		if left == "" {
			layer += fmt.Sprintf("%s label := %d\n", spaces, label)
		} else {
			layer += fmt.Sprintf("%s\n", left)
		}
		layer += fmt.Sprintf("%s}", spaces)
		return layer
	}
	return serialize(r, 0, 0)
}
