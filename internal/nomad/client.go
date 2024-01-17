package nomad

import (
	"context"
	"fmt"
	"sync"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/hashicorp/nomad/api"
	"github.com/n6g7/nomtail/internal/loki"
	"github.com/n6g7/nomtail/pkg/log"
)

const chanBufferSize int = 32

const (
	allocRefreshInterval   time.Duration = 10 * time.Second
	maxAllocRefreshRetries int           = 10
)

type client struct {
	c        *api.Client
	logger   *log.Logger
	nodeId   string
	nodeName string

	// Map of allocation IDs to alloc runner.
	// allocRunners[alloc.ID] = allocRunner{}
	allocRunners map[string]*allocRunner
	// Map of allocation IDs to maps of task name to task runner.
	// taskRunners[alloc.ID][task] = taskRunner{}
	taskRunners map[string]map[string]*taskRunner

	wg                      sync.WaitGroup
	entries                 chan loki.Entry
	failedAllocRefreshCount int
}

func NewClient(logger *log.Logger) (*client, error) {
	nomad, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize nomad client: %w", err)
	}

	client := client{
		c:                       nomad,
		logger:                  logger.With("component", "nomad"),
		allocRunners:            make(map[string]*allocRunner),
		taskRunners:             make(map[string]map[string]*taskRunner),
		entries:                 make(chan loki.Entry, chanBufferSize),
		failedAllocRefreshCount: 0,
	}

	if err = client.Init(); err != nil {
		return nil, err
	}
	return &client, nil
}

func (c *client) Init() error {
	self, err := c.c.Agent().Self()
	if err != nil {
		return fmt.Errorf("failed to read local agent data: %w", err)
	}

	c.nodeId = self.Stats["client"]["node_id"]
	c.nodeName = self.Config["NodeName"].(string)

	c.logger.Info("connected to nomad agent", "nodeName", c.nodeName, "nodeId", c.nodeId)

	return nil
}

func (c *client) getAllocations() ([]*api.Allocation, error) {
	allocs, _, err := c.c.Nodes().Allocations(c.nodeId, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to read node allocation: %w", err)
	}
	return allocs, nil
}

func (c *client) refreshAllocations(ctx context.Context) error {
	allocs, err := c.getAllocations()
	if err != nil {
		c.failedAllocRefreshCount += 1
		if c.failedAllocRefreshCount >= maxAllocRefreshRetries {
			return fmt.Errorf("failed to read allocations %d times, stopping. Last error: %w", c.failedAllocRefreshCount, err)
		}
		c.logger.WarnContext(ctx, "failed to read allocations from nomad", "err", err, "retries", c.failedAllocRefreshCount)
		return nil
	}
	c.failedAllocRefreshCount = 0

	c.logger.DebugContext(ctx, "got allocations from Nomad", "allocation_count", len(allocs))

	newAllocs := mapset.NewSet[string]()
	for _, alloc := range allocs {
		switch alloc.ClientStatus {
		// Skip allocations we can't get logs from.
		case api.AllocClientStatusComplete, api.AllocClientStatusPending,
			api.AllocClientStatusFailed, api.AllocClientStatusLost:
			continue
		case api.AllocClientStatusRunning:
		default:
			c.logger.WarnContext(ctx,
				"unknown allocation status",
				"allocation", alloc.ID,
				"client_status", alloc.ClientStatus,
				"client_description", alloc.ClientDescription,
				"desired_status", alloc.DesiredStatus,
				"desired_description", alloc.DesiredDescription,
			)
		}
		newAllocs.Add(alloc.ID)

		// New alloc
		if _, ok := c.allocRunners[alloc.ID]; !ok {
			c.allocRunners[alloc.ID] = startAllocRunner(ctx, c, alloc)
		}
		if _, ok := c.taskRunners[alloc.ID]; !ok {
			c.taskRunners[alloc.ID] = make(map[string]*taskRunner)
		}

		for task := range alloc.TaskStates {
			// New task
			if _, ok := c.taskRunners[alloc.ID][task]; !ok {
				c.taskRunners[alloc.ID][task] = startTaskRunner(ctx, c, alloc, task)
			}
		}

		// Stopped task
		for runningTask, runner := range c.taskRunners[alloc.ID] {
			if _, ok := alloc.TaskStates[runningTask]; !ok {
				runner.stop()
				delete(c.taskRunners[alloc.ID], runningTask)
			}
		}
	}

	// Stopped alloc
	for runningAlloc, runners := range c.taskRunners {
		if !newAllocs.Contains(runningAlloc) {
			// Stop task runners for this alloc
			for task, runner := range runners {
				runner.stop()
				delete(runners, task)
			}
			delete(c.taskRunners, runningAlloc)
		}
	}
	for runningAlloc, runner := range c.allocRunners {
		if !newAllocs.Contains(runningAlloc) {
			// Stop alloc runner
			runner.stop()
			delete(c.allocRunners, runningAlloc)
		}
	}

	return nil
}

func (c *client) Run(parentCtx context.Context, wg *sync.WaitGroup) error {
	defer wg.Done()

	ctx, cancel := context.WithCancel(parentCtx)
	exit := func() {
		cancel()
		c.wg.Wait()
		close(c.entries)
		c.logger.Info("bye")
	}

	allocationRefreshTicker := time.Tick(allocRefreshInterval)

	// First allocations load
	err := c.refreshAllocations(ctx)
	if err != nil {
		exit()
		return err
	}

	for {
		select {
		case <-parentCtx.Done():
			exit()
			return nil
		case <-allocationRefreshTicker:
			err := c.refreshAllocations(ctx)
			if err != nil {
				exit()
				return err
			}
		}
	}
}

func (c *client) Chan() <-chan loki.Entry {
	return c.entries
}
