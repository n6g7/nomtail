package nomad

import (
	"context"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/push"
	"github.com/hashicorp/nomad/api"
	"github.com/n6g7/nomtail/internal/log"
	"github.com/n6g7/nomtail/internal/loki"
	"github.com/prometheus/common/model"
)

const logStreamOrigin = "start"

type taskRunner struct {
	logger     *log.Logger
	client     *client
	alloc      *api.Allocation
	task       string
	taskLabels *model.LabelSet
	cancel     context.CancelFunc
}

func startTaskRunner(ctx context.Context, client *client, alloc *api.Allocation, task string) *taskRunner {
	ctx, cancel := context.WithCancel(ctx)

	tr := taskRunner{
		logger: client.logger.With("allocation", alloc.ID, "task", task),
		client: client,
		alloc:  alloc,
		task:   task,
		taskLabels: &model.LabelSet{
			model.LabelName("nomad_job_id"):     model.LabelValue(alloc.JobID),
			model.LabelName("nomad_job_name"):   model.LabelValue(*alloc.Job.Name),
			model.LabelName("nomad_task_group"): model.LabelValue(alloc.TaskGroup),
			model.LabelName("nomad_alloc_id"):   model.LabelValue(alloc.ID),
			model.LabelName("nomad_task_name"):  model.LabelValue(task),
		},
		cancel: cancel,
	}

	client.wg.Add(2)
	go tr.run(ctx, "stdout")
	go tr.run(ctx, "stderr")

	return &tr
}

func (r *taskRunner) sendLine(ts time.Time, line string, runLabels *model.LabelSet) {
	labels := r.taskLabels.Merge(*runLabels)

	r.client.entries <- loki.Entry{
		Entry: push.Entry{
			Timestamp: ts,
			Line:      line,
		},
		Labels: labels,
	}
}

func (r *taskRunner) run(ctx context.Context, logType string) {
	defer r.client.wg.Done()

	logger := r.logger.With("stream", logType)
	runLabels := model.LabelSet{
		model.LabelName("stream"): model.LabelValue(logType),
	}

	cancelChan := make(chan struct{})
	streamChan, errChan := r.client.c.AllocFS().Logs(r.alloc, true, r.task, logType, logStreamOrigin, 0, cancelChan, nil)
	stopLogs := func() {
		cancelChan <- struct{}{}
		close(cancelChan)
	}

	logger.InfoContext(ctx, "ingesting logs for task")

	currentLine := ""
	var currentLineFirstSeenOn time.Time

	for {
		select {
		case <-ctx.Done():
			logger.Info("stopping ingestion")
			stopLogs()
			return
		case frame, ok := <-streamChan:
			if !ok {
				logger.WarnContext(ctx, "stream channel closed unexpectedly. Does that mean the task/alloc was stopped? The runner should be restarted at the next allocation refresh if necessary.")
				stopLogs()
				return
			}
			frameTime := time.Now()

			// Need to convert to string to reliably detect newline chars.
			data := string(frame.Data)
			lines := strings.Split(data, "\n")
			// Prepend current line to first line
			lines[0] = currentLine + lines[0]
			// Send all lines but the last one (unfinished)
			for i, line := range lines[:len(lines)-1] {
				// Line time is either frame time of last frame time
				var lineTime time.Time
				if i == 0 && len(currentLine) > 0 {
					lineTime = currentLineFirstSeenOn
				} else {
					lineTime = frameTime
				}

				logger.TraceContext(ctx, "received new log line", "txt", line, "ts", lineTime)
				r.sendLine(lineTime, line, &runLabels)
			}
			// Store last line to be completed next iteration
			currentLine = lines[len(lines)-1]
			currentLineFirstSeenOn = frameTime

		case err, ok := <-errChan:
			if !ok {
				logger.WarnContext(ctx, "error channel closed unexpectedly?????")
				stopLogs()
				return
			}
			logger.WarnContext(ctx, "received error", "err", err)
		}
	}
}

func (r *taskRunner) stop() {
	r.cancel()
}
