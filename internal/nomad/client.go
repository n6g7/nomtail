package nomad

import (
	"context"
	"fmt"
	"sync"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/hashicorp/nomad/api"
	"github.com/n6g7/nomtail/internal/log"
	"github.com/n6g7/nomtail/internal/loki"
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

	// Map of allocation IDs to maps of task name to runner.
	// runners[alloc.ID][task] = runner{}
	runners map[string]map[string]*taskRunner
	wg      sync.WaitGroup

	entries chan loki.Entry

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
		runners:                 make(map[string]map[string]*taskRunner),
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
		newAllocs.Add(alloc.ID)

		// New alloc
		if _, ok := c.runners[alloc.ID]; !ok {
			c.runners[alloc.ID] = make(map[string]*taskRunner)
		}

		for task := range alloc.TaskStates {
			// New task
			if _, ok := c.runners[alloc.ID][task]; !ok {
				c.runners[alloc.ID][task] = startTaskRunner(ctx, c, alloc, task)
			}
		}

		// Stopped task
		for runningTask, runner := range c.runners[alloc.ID] {
			if _, ok := alloc.TaskStates[runningTask]; !ok {
				runner.stop()
			}
		}
	}

	// Stopped alloc
	for runningAlloc, runners := range c.runners {
		if !newAllocs.Contains(runningAlloc) {
			for _, runner := range runners {
				runner.stop()
			}
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
