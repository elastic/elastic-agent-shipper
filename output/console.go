package output

import (
	"fmt"
	"sync"

	"github.com/elastic/elastic-agent-shipper/api"
	"github.com/elastic/elastic-agent-shipper/queue"
)

type ConsoleOutput struct {
	queue *queue.Queue

	wg sync.WaitGroup
}

func NewConsoleOutput(queue *queue.Queue) *ConsoleOutput {
	return &ConsoleOutput{queue: queue}
}

func (out *ConsoleOutput) Start() {
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
				if event, ok := batch.Event(i).(*api.Event); ok {
					out.send(event)
				}
			}
			// This tells the queue that we're done with these events
			// and they can be safely discarded. The Beats queue interface
			// doesn't provide a way to indicate failure, either of the
			// full batch or of individual events. The plan is for the
			// shipper to track events by their queue IDs so outputs
			// can report status back to the server; see
			// https://github.com/elastic/elastic-agent-shipper/issues/27.
			batch.ACK()
		}
	}()
}

func (*ConsoleOutput) send(event *api.Event) {
	fmt.Printf("%v\n", event)
}

// Wait until the output loop has finished. This doesn't stop the
// loop by itself, so make sure you only call it when you close
// the queue.
func (out *ConsoleOutput) Wait() {
	out.wg.Wait()
}
