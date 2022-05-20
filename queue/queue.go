package queue

import (
	beatsqueue "github.com/elastic/beats/v7/libbeat/publisher/queue"

	"github.com/elastic/elastic-agent-shipper/api"
)

// Queue is a shipper-specific wrapper around the bare libbeat queue.
// It accepts api.Event instead of bare interface pointers like the
// libbeat queue, and it sets opinionated defaults for the queue
// semantics. The intention is to keep the shipper from becoming too
// entangled with the legacy queue api, and to gradually expose more
// features as the libbeat queue evolves and we decide what we want
// to support in the shipper.
type Queue struct {
	eventQueue beatsqueue.Queue

	producer beatsqueue.Producer
}

type Metrics beatsqueue.Metrics

// metricsSource is a wrapper around the libbeat queue interface, exposing only
// the callback to query the current metrics. It is used to pass queue metrics
// to the monitoring package.
type MetricsSource interface {
	Metrics() (Metrics, error)
}

func (queue *Queue) PublishOrSomething(event *api.Event) {

}
