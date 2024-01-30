package nomad

import (
	"context"
	"time"

	"github.com/grafana/loki/pkg/push"
	"github.com/hashicorp/nomad/api"
	"github.com/n6g7/nomtail/internal/loki"
	"github.com/n6g7/nomtail/pkg/log"
	"github.com/prometheus/common/model"
)

const (
	eventStream string        = "events"
	maxEventAge time.Duration = 12 * time.Hour
)

type allocRunner struct {
	logger      *log.Logger
	client      *client
	alloc       *api.Allocation
	allocLabels *model.LabelSet
	cancel      context.CancelFunc
}

func startAllocRunner(ctx context.Context, client *client, alloc *api.Allocation) *allocRunner {
	ctx, cancel := context.WithCancel(ctx)

	ar := allocRunner{
		logger: client.logger.With("allocation", alloc.ID, "stream", eventStream),
		client: client,
		alloc:  alloc,
		allocLabels: &model.LabelSet{
			model.LabelName("nomad_job_id"):     model.LabelValue(alloc.JobID),
			model.LabelName("nomad_job_name"):   model.LabelValue(*alloc.Job.Name),
			model.LabelName("nomad_task_group"): model.LabelValue(alloc.TaskGroup),
			model.LabelName("nomad_alloc_id"):   model.LabelValue(alloc.ID),
		},
		cancel: cancel,
	}

	client.wg.Add(1)
	go ar.run(ctx)

	return &ar
}

func (r *allocRunner) sendLine(ts time.Time, line string, runLabels *model.LabelSet) {
	labels := r.allocLabels.Merge(*runLabels)

	r.client.entries <- loki.Entry{
		Entry: push.Entry{
			Timestamp: ts,
			Line:      line,
		},
		Labels: labels,
	}
}

func (r *allocRunner) run(ctx context.Context) {
	defer r.client.wg.Done()

	streamCtx, cancel := context.WithCancel(ctx)
	eventsChan, err := r.client.c.EventStream().Stream(streamCtx, map[api.Topic][]string{
		api.TopicAllocation: {r.alloc.ID},
	}, 0, nil)
	if err != nil {
		r.logger.WarnContext(ctx, "Failed to open events stream", "err", err)
		cancel()
		return
	}

	r.logger.InfoContext(ctx, "ingesting events for alloc")

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("stopping ingestion")
			cancel()
			return
		case events, ok := <-eventsChan:
			if !ok {
				r.logger.WarnContext(ctx, "events channel closed unexpectedly. Does that mean the alloc was stopped? The runner should be restarted at the next allocation refresh if necessary.")
				cancel()
				return
			}

			// Pre-compute minimum event time.
			minTime := time.Now().Add(-maxEventAge)

			// Here comes a lot of interface casting
			for _, ev := range events.Events {
				allocation := ev.Payload["Allocation"].(map[string]interface{})

				// New allocations might not have tasks yet.
				if allocation["TaskStates"] == nil {
					r.logger.DebugContext(ctx, "allocation's `TaskStates` is nil", "allocation", allocation)
					continue
				}

				taskStates := allocation["TaskStates"].(map[string]interface{})
				for task, v := range taskStates {
					states := v.(map[string]interface{})
					events := states["Events"].([]interface{})
					for _, e := range events {
						event := e.(map[string]interface{})

						// Nomad sends nanosecond timestamps
						nstimestamp := event["Time"].(float64)
						sec := int64(nstimestamp / 1e9)
						nsec := int64(nstimestamp) % 1e9
						eventTime := time.Unix(sec, nsec)

						// Check that the event isn't too old.
						if eventTime.Before(minTime) {
							r.logger.DebugContext(ctx, "ignoring old event", "event_time", eventTime, "min_time", minTime, "task", task, "event", event)
							continue
						}

						r.logger.TraceContext(ctx, "received new event", "event_time", eventTime, "task", task, "event", event)

						r.sendLine(
							eventTime,
							event["DisplayMessage"].(string),
							&model.LabelSet{
								model.LabelName("nomad_task_name"): model.LabelValue(task),
								model.LabelName("event_type"):      model.LabelValue(event["Type"].(string)),
								model.LabelName("stream"):          model.LabelValue(eventStream),
							},
						)
					}
				}
			}
		}
	}
}

func (r *allocRunner) stop() {
	r.cancel()
}
