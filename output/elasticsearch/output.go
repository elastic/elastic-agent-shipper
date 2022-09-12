package elasticsearch

import (
	"fmt"
	"sync"

	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	"github.com/elastic/elastic-agent-shipper/queue"
)

type ElasticSearchOutput struct {
	logger *logp.Logger
	config *Config

	queue *queue.Queue

	wg sync.WaitGroup
}

func NewElasticSearch(config *Config, queue *queue.Queue) *ElasticSearchOutput {
	return &ElasticSearchOutput{
		logger: logp.NewLogger("elasticsearch-output"),
		config: config,
		queue:  queue,
	}
}

func (out *ElasticSearchOutput) Start() {
	out.wg.Add(1)
	go func() {
		defer out.wg.Done()
		for {
			batch, err := out.queue.Get(1000)
			// Once an output receives a batch, it is responsible for
			// it until all events have been either successfully sent or
			// discarded after failure.
			if err != nil {
				// queue.Get can only fail if the queue was closed,
				// time for the output to shut down.
				break
			}
			for i := 0; i < batch.Count(); i++ {
				if event, ok := batch.Entry(i).(*messages.Event); ok {
					out.send(event)
				}
			}
			// This tells the queue that we're done with these events
			// and they can be safely discarded. The Beats queue interface
			// doesn't provide a way to indicate failure, of either the
			// full batch or individual events. The plan is for the
			// shipper to track events by their queue IDs so outputs
			// can report status back to the server; see
			// https://github.com/elastic/elastic-agent-shipper/issues/27.
			batch.Done()
		}
	}()
}

func (*ElasticSearchOutput) send(event *messages.Event) {
	//nolint: forbidigo // Console output is intentional
	fmt.Printf("%v\n", event)
}

// Wait until the output loop has finished. This doesn't stop the
// loop by itself, so make sure you only call it when you close
// the queue.
func (out *ElasticSearchOutput) Wait() {
	out.wg.Wait()
}
