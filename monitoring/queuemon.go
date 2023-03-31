// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package monitoring

import (
	"encoding/json"
	"expvar"
	"fmt"
	"runtime"

	"github.com/elastic/beats/v7/libbeat/beat"
	libbeatreporter "github.com/elastic/beats/v7/libbeat/monitoring/report"
	libbeatlog "github.com/elastic/beats/v7/libbeat/monitoring/report/log"
	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-libs/api"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/monitoring"
	"github.com/elastic/elastic-agent-libs/opt"
	"github.com/elastic/elastic-agent-shipper/queue"
	"github.com/elastic/elastic-agent-shipper/tools"
	"github.com/elastic/elastic-agent-system-metrics/report"
)

type queueData struct {
	CurrentLevel      uint64   `json:"current_level"`
	MaxLevel          uint64   `json:"max_level"`
	IsFull            bool     `json:"is_full"`
	LimitReachedCount uint64   `json:"limit_reached_count"`
	UnackedRead       opt.Uint `json:"unacked_read"`
}

// QueueMonitor is the main handler object for the queue monitor, and will be responsible for startup, shutdown, handling config, and persistent tracking of metrics.
type QueueMonitor struct {
	// handler for the event queue
	target queue.MetricsSource
	// handler for the periodic metrics reporting in the logger
	logReporter libbeatreporter.Reporter
	// handler for the http server
	httpHandler *api.Server

	log *logp.Logger

	// Count of times the queue has reached a configured limit.
	queueLimitCount uint64

	// a private monitoring namespace. The global namespace doesn't support re-registering functions, which we need with shipper's stop-start model
	// The only downside to this is that the log reporter can't see it.
	shipperNS monitoring.Namespaces
}

func init() {
	setupRegisterInfo(logp.L())
}

type expvarHTTPCfg struct {
	Enabled bool `config:"expvar.enabled"`
}

// NewFromConfig creates a new queue monitor from a pre-filled config struct.
func NewFromConfig(httpCfg, logCfg *config.C, target queue.MetricsSource) (*QueueMonitor, error) {
	logger := logp.L()

	mon := &QueueMonitor{target: target, log: logger, shipperNS: *monitoring.NewNamespaces()}
	metricsReg := mon.shipperNS.Get("shipper").GetRegistry()

	monitoring.NewFunc(metricsReg, "shipper", mon.reportQueueMetrics, monitoring.Report)

	// create periodic reporting logger
	reporter, err := libbeatlog.MakeReporter(beat.Info{}, logCfg)
	if err != nil {
		return nil, fmt.Errorf("error making log reporter: %w", err)
	}
	mon.logReporter = reporter

	if httpCfg.Enabled() {
		logger.Debugf("starting http monitoring")
		apiSrv, err := api.NewWithDefaultRoutes(logger, httpCfg, monitoring.GetNamespace)
		if err != nil {
			return nil, fmt.Errorf("error creating API for registry reporter: %w", err)
		}

		apiSrv.AddRoute("/shipper", api.MakeAPIHandler(mon.shipperNS.Get("shipper")))

		// re-export expvars
		expCfg := expvarHTTPCfg{}
		err = httpCfg.Unpack(&expCfg)
		if err != nil {
			return nil, fmt.Errorf("error unpacking expvar config: %w", err)
		}
		if expCfg.Enabled {
			apiSrv.AddRoute("/debug/vars", expvar.Handler().ServeHTTP)
		}
		mon.httpHandler = apiSrv
		go func() {
			apiSrv.Start()
		}()
	}

	return mon, nil
}

// a callback passed to monitoring.NewFunc for reporting queue metrics
func (mon *QueueMonitor) reportQueueMetrics(_ monitoring.Mode, V monitoring.Visitor) {
	mon.log.Debugf("register called reportQueueMetrics")
	V.OnRegistryStart()
	defer V.OnRegistryFinished()

	metrics, err := mon.getQueueMetrics()
	if err != nil {
		mon.log.Errorf("error fetching queue metrics: %w", err)
		return
	}
	monitoring.ReportNamespace(V, "queue", func() {
		monitoring.ReportInt(V, "current_level", int64(metrics.CurrentLevel))
		monitoring.ReportInt(V, "max_level", int64(metrics.MaxLevel))
		monitoring.ReportBool(V, "is_full", metrics.IsFull)
		monitoring.ReportInt(V, "limit_reached_count", int64(metrics.LimitReachedCount))
		if metrics.UnackedRead.Exists() {
			monitoring.ReportInt(V, "unacked_read", int64(metrics.UnackedRead.ValueOr(0)))
		}

	})
}

// End closes the metrics reporter and associated interfaces.
func (mon *QueueMonitor) End() {
	if mon.httpHandler != nil {
		err := mon.httpHandler.Stop()
		if err != nil {
			mon.log.Errorf("error stopping HTTP handler for metrics: %s", err)
		}
	}

	mon.logReporter.Stop()
}

// DiagnosticsCallback returns a function that can be sent to a V2 unit's RegisterDiagnosticHook
func (mon *QueueMonitor) DiagnosticsCallback() client.DiagnosticHook {
	return func() []byte {
		metrics, err := mon.getQueueMetrics()
		if err != nil {
			return mon.diagCallbackError(err)
		}
		metricsJSON, err := json.MarshalIndent(metrics, "", " ")
		if err != nil {
			return mon.diagCallbackError(err)
		}
		return metricsJSON
	}
}

func (mon *QueueMonitor) getQueueMetrics() (queueData, error) {
	raw, err := mon.target.Metrics()
	if err != nil {
		return queueData{}, fmt.Errorf("error fetching queue Metrics: %w", err)
	}

	count, limit, queueIsFull, err := getLimits(raw)
	if err != nil {
		return queueData{}, fmt.Errorf("could not get queue metrics limits: %w", err)
	}

	// ideally this should be done on the queue side, since from here, it's hard to tell from periodic updates what the true count of "limit events" are
	// i.e, if we poll every second, and we see a full queue twice, has the queue been full for one second, or did it become full, drain, and then become full again in one second?
	if queueIsFull {
		mon.queueLimitCount = mon.queueLimitCount + 1
	}

	return queueData{
		CurrentLevel:      count,
		MaxLevel:          limit,
		IsFull:            queueIsFull,
		LimitReachedCount: mon.queueLimitCount,
		UnackedRead:       raw.UnackedConsumedEvents,
	}, nil

}

// diagcallbackError is a wrapper for handling errors in the V2 client diagnostics callback.
// I'm operating on the understanding that if something should return JSON, it must *always* return JSON.
func (mon *QueueMonitor) diagCallbackError(origErr error) []byte {
	mon.log.Errorf("Got error while fetching metrics for elastic-agent diagnostics: %s", origErr)
	jsonErr, err := json.MarshalIndent(map[string]string{"error fetching queue metrics": origErr.Error()}, "", " ")
	if err != nil {
		mon.log.Errorf("error marshalling error response from DiagnosticsCallback(): %s", err)
	}
	return jsonErr
}

// create the basic monitoring namespaces
func setupRegisterInfo(logger *logp.Logger) {
	name := "shipper"
	version := tools.GetDefaultVersion()

	//init common metrics
	err := report.SetupMetrics(logger, name, version)
	if err != nil {
		logger.Errorf("error setting up basic monitoring metrics: %w", err)
	}

	infoRegistry := monitoring.GetNamespace("info").GetRegistry()
	monitoring.NewString(infoRegistry, "version").Set(version)
	monitoring.NewString(infoRegistry, "name").Set(name)
	monitoring.NewString(infoRegistry, "ephemeral_id").Set(report.EphemeralID().String())
	monitoring.NewString(infoRegistry, "binary_arch").Set(runtime.GOARCH)
	monitoring.NewString(infoRegistry, "build_commit").Set(tools.Commit())
	monitoring.NewTimestamp(infoRegistry, "build_time").Set(tools.BuildTime())

	// Beats set this if it's an xpack build
	// Is this implied in the shipper?
	// monitoring.NewBool(infoRegistry, "elastic_licensed").Set(b.Info.ElasticLicensed)

	// set user data
	report.SetupInfoUserMetrics()

	stateRegistry := monitoring.GetNamespace("state").GetRegistry()

	// state.service
	serviceRegistry := stateRegistry.NewRegistry("service")
	monitoring.NewString(serviceRegistry, "version").Set(version)
	monitoring.NewString(serviceRegistry, "name").Set(name)

	// state.beat
	beatRegistry := stateRegistry.NewRegistry("beat")
	monitoring.NewString(beatRegistry, "name").Set(name)
}

// This is a wrapper to deal with the multiple queue metric "types",
// as we could either be dealing with event counts, or bytes.
// The reporting interfaces assumes we only want one.
func getLimits(raw queue.Metrics) (uint64, uint64, bool, error) {

	//bias towards byte count, as it's a little more granular.
	if raw.ByteCount.Exists() && raw.ByteLimit.Exists() {
		count := raw.ByteCount.ValueOr(0)
		limit := raw.ByteLimit.ValueOr(0)
		// As @faec has noted, calculating limits can be a bit awkward when we're dealing with reporting/configuration in bytes.
		//All we have now is total queue size in bytes, which doesn't tell us how many more events could fit before we hit the queue limit.
		//So until we have something better, mark anything as 90% or more full as "full"

		// I'm assuming that limit can be zero here, as perhaps a user can configure a queue without a limit, and it gets passed down to us.
		if limit == 0 {
			return count, limit, false, nil
		}
		level := float64(count) / float64(limit)
		return count, limit, level > 0.9, nil
	}

	if raw.EventCount.Exists() && raw.EventLimit.Exists() {
		count := raw.EventCount.ValueOr(0)
		limit := raw.EventLimit.ValueOr(0)
		return count, limit, count >= limit, nil
	}

	return 0, 0, false, fmt.Errorf("could not find valid byte or event metrics in queue")
}
