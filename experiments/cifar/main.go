package main

import (
	"flag"
	"log"
	"math/rand"
	"time"

	"github.com/unixpickle/anynet"
	"github.com/unixpickle/anynet/anyconv"
	"github.com/unixpickle/anynet/anyff"
	"github.com/unixpickle/anynet/anysgd"
	"github.com/unixpickle/anyvec"
	"github.com/unixpickle/anyvec/anyvec32"
	"github.com/unixpickle/cifar"
	"github.com/unixpickle/essentials"
	"github.com/unixpickle/rip"
	"github.com/unixpickle/serializer"
	"github.com/unixpickle/slidingrbf"
)

var Creator anyvec.Creator

func main() {
	Creator = anyvec32.CurrentCreator()
	rand.Seed(time.Now().UnixNano())

	var sampleDir string
	var netPath string
	var stepSize float64
	var batchSize int
	var successRate bool

	flag.StringVar(&sampleDir, "samples", "", "cifar-10 binary batch dir")
	flag.StringVar(&netPath, "net", "out_net", "network file")
	flag.Float64Var(&stepSize, "step", 0.001, "SGD step size")
	flag.IntVar(&batchSize, "batch", 64, "SGD batch size")
	flag.BoolVar(&successRate, "successrate", false, "print success rate")
	flag.Parse()

	if sampleDir == "" {
		essentials.Die("Missing -samples flag. See -help.")
	}

	lists, err := cifar.Load10(sampleDir)
	if err != nil {
		essentials.Die(err)
	}

	training := cifar.NewSampleListAll(Creator, lists[:5]...)
	validation := cifar.NewSampleListAll(Creator, lists[5])

	var net anynet.Net
	if err := serializer.LoadAny(netPath, &net); err != nil {
		log.Println("Creating new network...")
		net = anynet.Net{
			slidingrbf.NewLayer(Creator, 32, 32, 3, 3, 3, 8, 2, 2),
			anyconv.NewBatchNorm(Creator, 8),
			slidingrbf.NewLayer(Creator, 15, 15, 8, 4, 4, 8, 1, 1),
			anyconv.NewBatchNorm(Creator, 8),
			slidingrbf.NewLayer(Creator, 12, 12, 8, 3, 3, 16, 2, 2),
			anyconv.NewBatchNorm(Creator, 16),
			anynet.NewFC(Creator, 16*5*5, 10),
			anynet.LogSoftmax,
		}
	} else {
		log.Println("Using existing network.")
	}

	if successRate {
		log.Println("Computing success rate...")
		printSuccessRate(net, validation, batchSize)
	}

	log.Println("Setting up...")

	Creator = anyvec32.CurrentCreator()

	t := &anyff.Trainer{
		Net:     net,
		Cost:    anynet.DotCost{},
		Params:  net.Parameters(),
		Average: true,
	}

	var iterNum int
	s := &anysgd.SGD{
		Fetcher:     t,
		Gradienter:  t,
		Transformer: &anysgd.Adam{},
		Samples:     training,
		Rater:       anysgd.ConstRater(0.001),
		StatusFunc: func(b anysgd.Batch) {
			anysgd.Shuffle(validation)
			batch, _ := t.Fetch(validation.Slice(0, batchSize))
			vCost := anyvec.Sum(t.TotalCost(batch.(*anyff.Batch)).Output())

			log.Printf("iter %d: cost=%v validation=%v", iterNum, t.LastCost, vCost)
			iterNum++
		},
		BatchSize: 200,
	}

	log.Println("Press ctrl+c once to stop...")
	s.Run(rip.NewRIP().Chan())

	log.Println("Saving network...")
	if err := serializer.SaveAny(netPath, net); err != nil {
		essentials.Die(err)
	}
}

func printSuccessRate(net anynet.Net, samples anysgd.SampleList, batch int) {
	ones := anyvec32.MakeVector(batch)
	ones.AddScaler(float32(1))

	fetcher := &anyff.Trainer{}
	correct := 0.0
	total := 0.0
	for i := 0; i+batch <= samples.Len(); i += batch {
		b, _ := fetcher.Fetch(samples.Slice(i, i+batch))
		ins := b.(*anyff.Batch).Inputs
		desired := b.(*anyff.Batch).Outputs.Output()
		outs := net.Apply(ins, batch).Output()

		mapper := anyvec.MapMax(outs, 10)
		maxes := anyvec32.MakeVector(desired.Len())
		mapper.MapTranspose(ones, maxes)

		correct += float64(maxes.Dot(desired).(float32))
		total += float64(batch)
	}

	log.Printf("Got %.3f%%", 100*correct/total)
}