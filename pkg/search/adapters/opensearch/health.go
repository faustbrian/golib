package opensearch

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
)

type HealthStatus string

const (
	HealthGreen  HealthStatus = "green"
	HealthYellow HealthStatus = "yellow"
	HealthRed    HealthStatus = "red"
)

// HealthReport is the bounded readiness view. Yellow is ready when primary
// shards are active and no shard initialization or timeout is in progress.
type HealthReport struct {
	Status              HealthStatus
	Ready               bool
	TimedOut            bool
	Nodes               int
	DataNodes           int
	ActivePrimaryShards int
	ActiveShards        int
	RelocatingShards    int
	InitializingShards  int
	UnassignedShards    int
	PendingTasks        int
	ActiveShardsPercent float64
}

func (c *Client) Health(ctx context.Context) (HealthReport, error) {
	body, err := c.execute(ctx, OperationHealth, http.MethodGet, "/_cluster/health?level=cluster", nil, http.StatusOK)
	if err != nil {
		return HealthReport{}, err
	}
	var payload struct {
		ClusterName         string       `json:"cluster_name"`
		Status              HealthStatus `json:"status"`
		TimedOut            *bool        `json:"timed_out"`
		Nodes               *int         `json:"number_of_nodes"`
		DataNodes           *int         `json:"number_of_data_nodes"`
		ActivePrimaryShards *int         `json:"active_primary_shards"`
		ActiveShards        *int         `json:"active_shards"`
		RelocatingShards    *int         `json:"relocating_shards"`
		InitializingShards  *int         `json:"initializing_shards"`
		UnassignedShards    *int         `json:"unassigned_shards"`
		PendingTasks        *int         `json:"number_of_pending_tasks"`
		ActiveShardsPercent *float64     `json:"active_shards_percent_as_number"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.ClusterName == "" ||
		payload.TimedOut == nil || payload.Nodes == nil || payload.DataNodes == nil ||
		payload.ActivePrimaryShards == nil || payload.ActiveShards == nil || payload.RelocatingShards == nil ||
		payload.InitializingShards == nil || payload.UnassignedShards == nil || payload.PendingTasks == nil || payload.ActiveShardsPercent == nil ||
		(payload.Status != HealthGreen && payload.Status != HealthYellow && payload.Status != HealthRed) ||
		*payload.Nodes < 0 || *payload.DataNodes < 0 || *payload.DataNodes > *payload.Nodes ||
		*payload.ActivePrimaryShards < 0 || *payload.ActiveShards < 0 || *payload.RelocatingShards < 0 ||
		*payload.InitializingShards < 0 || *payload.UnassignedShards < 0 || *payload.PendingTasks < 0 ||
		*payload.ActiveShardsPercent < 0 || *payload.ActiveShardsPercent > 100 {
		return HealthReport{}, malformedFailure(OperationHealth, ErrMalformedResponse)
	}
	return HealthReport{
		Status:   payload.Status,
		Ready:    !*payload.TimedOut && payload.Status != HealthRed && *payload.DataNodes > 0 && *payload.ActivePrimaryShards > 0 && *payload.InitializingShards == 0,
		TimedOut: *payload.TimedOut, Nodes: *payload.Nodes, DataNodes: *payload.DataNodes,
		ActivePrimaryShards: *payload.ActivePrimaryShards, ActiveShards: *payload.ActiveShards,
		RelocatingShards: *payload.RelocatingShards, InitializingShards: *payload.InitializingShards,
		UnassignedShards: *payload.UnassignedShards, PendingTasks: *payload.PendingTasks,
		ActiveShardsPercent: *payload.ActiveShardsPercent,
	}, nil
}

// CapacityReport exposes bounded low-cardinality saturation counters without
// retaining node IDs, index names, tenants, queries, or document fields.
type CapacityReport struct {
	Nodes              int
	DataNodes          int
	Indices            int
	Shards             int
	PrimaryShards      int
	Documents          uint64
	DeletedDocuments   uint64
	StoreBytes         uint64
	HeapUsedBytes      uint64
	HeapMaxBytes       uint64
	DiskAvailableBytes uint64
	ThreadPoolRejected map[string]uint64
	BreakerTripped     map[string]uint64
}

func (c *Client) Capacity(ctx context.Context) (CapacityReport, error) {
	clusterBody, err := c.execute(ctx, OperationCapacity, http.MethodGet, "/_cluster/stats?human=false", nil, http.StatusOK)
	if err != nil {
		return CapacityReport{}, err
	}
	var cluster struct {
		NodesResult struct{ Total, Successful, Failed int } `json:"_nodes"`
		Indices     struct {
			Count  int                             `json:"count"`
			Shards struct{ Total, Primaries int }  `json:"shards"`
			Docs   struct{ Count, Deleted uint64 } `json:"docs"`
			Store  struct {
				Size uint64 `json:"size_in_bytes"`
			} `json:"store"`
		} `json:"indices"`
		Nodes struct {
			Count struct{ Total, Data int } `json:"count"`
			JVM   struct {
				Memory struct {
					HeapUsed uint64 `json:"heap_used_in_bytes"`
					HeapMax  uint64 `json:"heap_max_in_bytes"`
				} `json:"mem"`
			} `json:"jvm"`
			FS struct {
				Available uint64 `json:"available_in_bytes"`
			} `json:"fs"`
		} `json:"nodes"`
	}
	if json.Unmarshal(clusterBody, &cluster) != nil || cluster.NodesResult.Total <= 0 || cluster.NodesResult.Failed != 0 || cluster.NodesResult.Successful != cluster.NodesResult.Total ||
		cluster.Nodes.Count.Total <= 0 || cluster.Nodes.Count.Data < 0 || cluster.Nodes.Count.Data > cluster.Nodes.Count.Total || cluster.Indices.Count < 0 || cluster.Indices.Shards.Total < 0 || cluster.Indices.Shards.Primaries < 0 {
		return CapacityReport{}, malformedFailure(OperationCapacity, ErrMalformedResponse)
	}
	nodesBody, err := c.execute(ctx, OperationCapacity, http.MethodGet, "/_nodes/stats/thread_pool,breaker", nil, http.StatusOK)
	if err != nil {
		return CapacityReport{}, err
	}
	var nodes struct {
		Result struct{ Total, Successful, Failed int } `json:"_nodes"`
		Nodes  map[string]struct {
			ThreadPool map[string]struct {
				Rejected uint64 `json:"rejected"`
			} `json:"thread_pool"`
			Breakers map[string]struct {
				Tripped uint64 `json:"tripped"`
			} `json:"breakers"`
		} `json:"nodes"`
	}
	if json.Unmarshal(nodesBody, &nodes) != nil || nodes.Result.Total <= 0 || nodes.Result.Failed != 0 || nodes.Result.Successful != nodes.Result.Total || len(nodes.Nodes) > MaximumDiscoveredNodes {
		return CapacityReport{}, malformedFailure(OperationCapacity, ErrMalformedResponse)
	}
	threadPools, breakers := make(map[string]uint64), make(map[string]uint64)
	for _, node := range nodes.Nodes {
		for name, stats := range node.ThreadPool {
			if safeMetricName(name) {
				if math.MaxUint64-threadPools[name] < stats.Rejected {
					return CapacityReport{}, malformedFailure(OperationCapacity, ErrMalformedResponse)
				}
				threadPools[name] += stats.Rejected
			}
		}
		for name, stats := range node.Breakers {
			if safeMetricName(name) {
				if math.MaxUint64-breakers[name] < stats.Tripped {
					return CapacityReport{}, malformedFailure(OperationCapacity, ErrMalformedResponse)
				}
				breakers[name] += stats.Tripped
			}
		}
	}
	return CapacityReport{
		Nodes: cluster.Nodes.Count.Total, DataNodes: cluster.Nodes.Count.Data,
		Indices: cluster.Indices.Count, Shards: cluster.Indices.Shards.Total, PrimaryShards: cluster.Indices.Shards.Primaries,
		Documents: cluster.Indices.Docs.Count, DeletedDocuments: cluster.Indices.Docs.Deleted, StoreBytes: cluster.Indices.Store.Size,
		HeapUsedBytes: cluster.Nodes.JVM.Memory.HeapUsed, HeapMaxBytes: cluster.Nodes.JVM.Memory.HeapMax, DiskAvailableBytes: cluster.Nodes.FS.Available,
		ThreadPoolRejected: threadPools, BreakerTripped: breakers,
	}, nil
}

func safeMetricName(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}
